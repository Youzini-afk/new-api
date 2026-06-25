package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type syncDiscordAvatarRequest struct {
	Force bool `json:"force"`
}

const (
	avatarMaxBytes     = service.AvatarMaxBytes
	avatarMaxBodyBytes = service.AvatarMaxBodyBytes
	avatarFormField    = service.AvatarFormField
)

// uploadSelfAvatarResponse is the contract returned by POST /api/user/self/avatar.
type uploadSelfAvatarResponse struct {
	AvatarURL    string `json:"avatar_url"`
	AvatarSource string `json:"avatar_source"`
}

// UploadSelfAvatar handles POST /api/user/self/avatar (multipart field `avatar`).
//
// The handler does NOT trust the client-reported Content-Type or filename
// extension: the uploaded bytes are run through image.DecodeConfig (and a
// full image.Decode) so a file must be a syntactically valid PNG or JPEG to be
// accepted. SVG / GIF / HTML / data URLs / truncated images all fail decode
// and are rejected.
func UploadSelfAvatar(c *gin.Context) {
	userID := c.GetInt("id")
	if userID == 0 {
		common.ApiErrorMsg(c, "user not authenticated")
		return
	}

	// Handler-level guard: even if the route-level body limit middleware is
	// bypassed somehow, refuse to process a body larger than the cap.
	if c.Request.ContentLength > avatarMaxBodyBytes {
		c.AbortWithStatus(http.StatusRequestEntityTooLarge)
		return
	}

	// Bound total body for handlers that didn't go through the route
	// middleware (e.g. direct test invocation). Wrapping here also stops gin
	// from buffering an oversized multipart payload to disk.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, avatarMaxBodyBytes)

	fileHeader, err := c.FormFile(avatarFormField)
	if err != nil {
		common.ApiErrorMsg(c, "avatar file is required (multipart field 'avatar')")
		return
	}

	if fileHeader.Size > avatarMaxBytes {
		common.ApiErrorMsg(c, fmt.Sprintf("avatar file too large: %d bytes (max %d)", fileHeader.Size, avatarMaxBytes))
		return
	}
	if fileHeader.Size == 0 {
		common.ApiErrorMsg(c, "avatar file is empty")
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		common.ApiErrorMsg(c, "failed to open uploaded avatar: "+err.Error())
		return
	}
	defer file.Close()

	// Hard cap again on the actual bytes we read — Size is client-reported
	// for multipart and not authoritative. LimitReader also bounds memory.
	data, err := io.ReadAll(io.LimitReader(file, avatarMaxBytes+1))
	if err != nil {
		common.ApiErrorMsg(c, "failed to read uploaded avatar: "+err.Error())
		return
	}
	if int64(len(data)) > avatarMaxBytes {
		common.ApiErrorMsg(c, fmt.Sprintf("avatar file too large (max %d bytes)", avatarMaxBytes))
		return
	}

	avatar, err := service.ValidateAvatarImage(userID, data)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if _, err := model.StoreUserAvatarWithSourceGuard(userID, avatar.ContentType, avatar.SHA256, avatar.Data, avatar.AvatarURL, model.AvatarSourceUploaded, true); err != nil {
		common.SysLog(fmt.Sprintf("failed to store avatar for user %d: %v", userID, err))
		common.ApiErrorMsg(c, "failed to store avatar")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": uploadSelfAvatarResponse{
			AvatarURL:    avatar.AvatarURL,
			AvatarSource: model.AvatarSourceUploaded,
		},
	})
}

// GetUserAvatar handles GET /api/user/avatar/:id/:hash.
//
// Public (no login): avatar URLs are meant to be embedded in <img> tags.
// A caller without the current SHA256 cache-busting token cannot retrieve the
// bytes through an old/stale URL. Responses are marked immutable because the URL
// changes whenever the avatar changes.
func GetUserAvatar(c *gin.Context) {
	idStr := c.Param("id")
	userID, err := parseIntParam(idStr)
	if err != nil || userID <= 0 {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	hashRaw := c.Param("hash")
	sha := stripExtension(hashRaw)
	if !isValidSHA256(sha) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	avatar, err := model.GetUserAvatarByUserAndHash(userID, sha)
	if err != nil {
		if errors.Is(err, model.ErrAvatarNotFound) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		common.SysLog(fmt.Sprintf("failed to load avatar for user %d: %v", userID, err))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	c.Header("Content-Type", avatar.ContentType)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Data(http.StatusOK, avatar.ContentType, avatar.Data)
}

// DeleteSelfAvatar handles DELETE /api/user/self/avatar.
//
// Removes the stored blob and clears avatar_url / avatar_source on the users
// row so subsequent GetSelf / setupLogin responses return empty fields.
func DeleteSelfAvatar(c *gin.Context) {
	userID := c.GetInt("id")
	if userID == 0 {
		common.ApiErrorMsg(c, "user not authenticated")
		return
	}

	if err := model.ClearUserAvatar(userID); err != nil {
		common.SysLog(fmt.Sprintf("failed to delete avatar blob for user %d: %v", userID, err))
		common.ApiErrorMsg(c, "failed to delete avatar")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": uploadSelfAvatarResponse{
			AvatarURL:    "",
			AvatarSource: "",
		},
	})
}

// SyncSelfDiscordAvatar handles POST /api/user/self/avatar/discord. It lets a
// user explicitly import their Discord avatar. force=true is user-initiated and
// may overwrite an uploaded avatar; force=false respects the same protection as
// automatic OAuth sync.
func SyncSelfDiscordAvatar(c *gin.Context) {
	userID := c.GetInt("id")
	if userID == 0 {
		common.ApiErrorMsg(c, "user not authenticated")
		return
	}
	var req syncDiscordAvatarRequest
	if c.Request != nil && c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			common.ApiErrorMsg(c, "invalid request body")
			return
		}
	}
	user, err := model.GetUserById(userID, true)
	if err != nil {
		common.ApiErrorMsg(c, "user not found")
		return
	}
	result, err := service.SyncDiscordAvatar(c.Request.Context(), user, req.Force)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrDiscordAvatarMissingBinding):
			common.ApiErrorMsg(c, "Discord account is not bound")
		case errors.Is(err, service.ErrDiscordAvatarMissingAvatar):
			common.ApiErrorMsg(c, "Discord avatar is not available")
		case errors.Is(err, service.ErrDiscordAvatarDownloadFailed):
			common.ApiErrorMsg(c, "failed to download Discord avatar")
		case errors.Is(err, service.ErrDiscordAvatarInvalidImage):
			common.ApiErrorMsg(c, "Discord avatar is not a valid image")
		default:
			common.SysLog(fmt.Sprintf("failed to sync Discord avatar for user %d: %v", userID, err))
			common.ApiErrorMsg(c, "failed to sync Discord avatar")
		}
		return
	}
	common.ApiSuccess(c, result)
}

// parseIntParam parses a route param into a positive int without panicking on
// bad input.
func parseIntParam(s string) (int, error) {
	return strconv.Atoi(s)
}

// stripExtension removes a trailing .ext from the hash param. The URL form is
// /api/user/avatar/{id}/{sha}.png — gin's :hash captures the whole segment.
func stripExtension(s string) string {
	if i := strings.LastIndexByte(s, '.'); i > 0 {
		return s[:i]
	}
	return s
}

// isValidSHA256 returns true only for a 64-char lowercase hex string, which
// is the form produced by hex.EncodeToString over sha256 output.
func isValidSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}
