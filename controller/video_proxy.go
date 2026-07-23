package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/governance"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// videoProxyError returns a standardized OpenAI-style error response.
func videoProxyError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errType,
		},
	})
}

const (
	videoProxyPrefixLimit     = 4 << 10
	videoProxyMinInspectBytes = 64
)

func videoProxyUpstreamError(c *gin.Context, err error, code types.ErrorCode) {
	upstreamErr := types.NewOpenAIError(err, code, http.StatusBadGateway)
	safe := governance.SanitizeRelayErrorForClient(c, upstreamErr)
	governance.RecordRelayErrorInsight(c, upstreamErr, safe, "video_proxy", "fetch_response", 0, 0)
	logger.LogError(c.Request.Context(), "video proxy upstream error: "+common.LocalLogPreview(upstreamErr.MaskSensitiveError()))
	videoProxyError(c, safe.StatusCode, safe.OpenAIError.Type, safe.OpenAIError.Message)
}

func readVideoProxyPrefix(body io.Reader) ([]byte, error) {
	if body == nil {
		return nil, io.ErrUnexpectedEOF
	}
	prefix := make([]byte, 0, videoProxyPrefixLimit)
	for len(prefix) < videoProxyPrefixLimit {
		buffer := make([]byte, videoProxyPrefixLimit-len(prefix))
		n, err := body.Read(buffer)
		if n > 0 {
			prefix = append(prefix, buffer[:n]...)
			trimmed := bytes.TrimSpace(prefix)
			// Structured error bodies can be rejected from their first significant
			// byte. Otherwise collect a small sample so a transport that happens to
			// return one byte at a time cannot bypass plain-text detection.
			if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[' || trimmed[0] == '<') {
				return prefix, nil
			}
			if len(prefix) >= videoProxyMinInspectBytes {
				return prefix, nil
			}
		}
		if err != nil {
			if err == io.EOF {
				return prefix, nil
			}
			return prefix, err
		}
		if n == 0 {
			return prefix, io.ErrNoProgress
		}
	}
	return prefix, nil
}

func validateVideoProxyPayload(contentType string, prefix []byte) error {
	trimmed := bytes.TrimSpace(prefix)
	if len(trimmed) == 0 {
		return fmt.Errorf("upstream video response was empty")
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "" {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(prefix), ";")[0]))
	}
	allowedMediaType := strings.HasPrefix(mediaType, "video/") ||
		mediaType == "application/octet-stream" || mediaType == "binary/octet-stream" ||
		mediaType == "application/mp4" || mediaType == "application/mpeg4" ||
		mediaType == "application/vnd.apple.mpegurl" || mediaType == "application/x-mpegurl" ||
		mediaType == "application/x-binary"
	if !allowedMediaType {
		return fmt.Errorf("upstream video returned unexpected content type %q", mediaType)
	}

	// A provider can incorrectly label a JSON/HTML/plain-text error as video.
	// Reject obvious text before any headers or bytes are committed downstream.
	if bytes.HasPrefix(bytes.ToUpper(trimmed), []byte("#EXTM3U")) {
		return nil
	}
	if trimmed[0] == '{' || trimmed[0] == '[' || trimmed[0] == '<' {
		return fmt.Errorf("upstream video returned a textual error payload: %s", trimmed)
	}
	sample := trimmed
	if len(sample) > 512 {
		sample = sample[:512]
	}
	printable := 0
	for _, value := range sample {
		if value == '\n' || value == '\r' || value == '\t' || (value >= 0x20 && value <= 0x7e) {
			printable++
		}
	}
	if len(sample) >= 8 && printable*100/len(sample) >= 90 {
		return fmt.Errorf("upstream video returned a plain-text payload: %s", sample)
	}
	return nil
}

func VideoProxy(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "task_id is required")
		return
	}

	userID := c.GetInt("id")
	task, exists, err := model.GetByTaskId(userID, taskID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to query task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to query task")
		return
	}
	if !exists || task == nil {
		videoProxyError(c, http.StatusNotFound, "invalid_request_error", "Task not found")
		return
	}

	if task.Status != model.TaskStatusSuccess {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("Task is not completed yet, current status: %s", task.Status))
		return
	}

	channel, err := model.CacheGetChannel(task.ChannelId)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to get channel for task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to retrieve channel information")
		return
	}
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	var videoURL string
	proxy := channel.GetSetting().Proxy
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create proxy client for task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy client")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "", nil)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create request: %s", err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}

	switch channel.Type {
	case constant.ChannelTypeGemini:
		apiKey := task.PrivateData.Key
		if apiKey == "" {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Missing stored API key for Gemini task %s", taskID))
			videoProxyError(c, http.StatusInternalServerError, "server_error", "API key not stored for task")
			return
		}
		videoURL, err = getGeminiVideoURL(ctx, channel, task, apiKey)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve Gemini video URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve Gemini video URL")
			return
		}
		req.Header.Set("x-goog-api-key", apiKey)
	case constant.ChannelTypeVertexAi:
		videoURL, err = getVertexVideoURL(ctx, channel, task)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve Vertex video URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve Vertex video URL")
			return
		}
	case constant.ChannelTypeOpenAI, constant.ChannelTypeSora:
		videoURL = fmt.Sprintf("%s/v1/videos/%s/content", baseURL, task.GetUpstreamTaskID())
		req.Header.Set("Authorization", "Bearer "+channel.Key)
	default:
		// Video URL is stored in PrivateData.ResultURL (fallback to FailReason for old data)
		videoURL = task.GetResultURL()
	}

	videoURL = strings.TrimSpace(videoURL)
	if videoURL == "" {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Video URL is empty for task %s", taskID))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}

	if strings.HasPrefix(videoURL, "data:") {
		if err := writeVideoDataURL(c, videoURL); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to decode video data URL for task %s: %s", taskID, common.MaskSensitiveInfo(err.Error())))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		}
		return
	}

	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(videoURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Video URL blocked for task %s: %v", taskID, err))
		videoProxyError(c, http.StatusForbidden, "server_error", fmt.Sprintf("request blocked: %v", err))
		return
	}

	req.URL, err = url.Parse(videoURL)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to parse URL %s: %s", videoURL, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}

	otherSettings := channel.GetOtherSettings()
	resp, err := service.DoChannelTrafficHTTPRequest(ctx, channel.Id, otherSettings.TrafficControl, func() (*http.Response, error) {
		return client.Do(req)
	})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to fetch video from %s: %s", videoURL, err.Error()))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		videoProxyUpstreamError(c,
			fmt.Errorf("upstream video returned status %d for %s", resp.StatusCode, videoURL),
			types.ErrorCodeBadResponseStatusCode,
		)
		return
	}

	prefix, err := readVideoProxyPrefix(resp.Body)
	if err != nil {
		videoProxyUpstreamError(c, fmt.Errorf("failed to read upstream video response: %w", err), types.ErrorCodeReadResponseBodyFailed)
		return
	}
	if err := validateVideoProxyPayload(resp.Header.Get("Content-Type"), prefix); err != nil {
		videoProxyUpstreamError(c, err, types.ErrorCodeBadResponseBody)
		return
	}

	for key, values := range resp.Header {
		if !service.ShouldCopyUpstreamHeader(c, key, values) {
			continue
		}
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}

	c.Writer.Header().Set("Cache-Control", "public, max-age=86400")
	c.Writer.WriteHeader(resp.StatusCode)
	if _, err = io.Copy(c.Writer, io.MultiReader(bytes.NewReader(prefix), resp.Body)); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to stream video content: %s", err.Error()))
	}
}

func writeVideoDataURL(c *gin.Context, dataURL string) error {
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid data url")
	}

	header := parts[0]
	payload := parts[1]
	if !strings.HasPrefix(header, "data:") || !strings.Contains(header, ";base64") {
		return fmt.Errorf("unsupported data url")
	}

	mimeType := strings.TrimPrefix(header, "data:")
	mimeType = strings.TrimSuffix(mimeType, ";base64")
	if mimeType == "" {
		mimeType = "video/mp4"
	}

	videoBytes, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		videoBytes, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return err
		}
	}
	inspect := videoBytes
	if len(inspect) > videoProxyPrefixLimit {
		inspect = inspect[:videoProxyPrefixLimit]
	}
	if err := validateVideoProxyPayload(mimeType, inspect); err != nil {
		return err
	}

	c.Writer.Header().Set("Content-Type", mimeType)
	c.Writer.Header().Set("Cache-Control", "public, max-age=86400")
	c.Writer.WriteHeader(http.StatusOK)
	_, err = c.Writer.Write(videoBytes)
	return err
}
