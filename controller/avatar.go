package controller

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const (
	// avatarMaxBytes is the hard cap on the decoded avatar file itself.
	// Frontends are expected to compress before upload; the backend rejects
	// anything larger so a malicious or buggy client cannot bloat the DB.
	avatarMaxBytes = 512 * 1024 // 512 KiB

	// avatarMaxBodyBytes bounds the whole multipart request body (form fields
	// + file). Applied as a route-level middleware; mirrored here for the
	// fallback handler-level guard.
	avatarMaxBodyBytes = 5 * 1024 * 1024 // 5 MiB

	// avatarMaxDim caps width / height in pixels.
	avatarMaxDim = 2048

	// avatarMaxPixels caps total pixels (width * height) to bound decode cost.
	avatarMaxPixels = 4 * 1024 * 1024 // 4 megapixels

	// avatarFormField is the multipart form field name clients must use.
	avatarFormField = "avatar"

	avatarSourceUploaded = "uploaded"
)

// formatMeta maps an image.DecodeConfig format string to its content-type and
// URL extension. Only entries here are accepted — SVG, GIF, HTML, data URLs
// and remote URLs are rejected by virtue of not being a registered/accepted
// decode format.
var formatMeta = map[string]struct {
	ContentType string
	Ext         string
}{
	"png":  {"image/png", "png"},
	"jpeg": {"image/jpeg", "jpg"},
}

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

	// Reject obvious non-image payloads by sniffing magic bytes before decode.
	// Defends against HTML / SVG / data: URLs that happen to parse cleanly.
	if msg := rejectNonImageMagic(data); msg != "" {
		common.ApiErrorMsg(c, msg)
		return
	}

	// DecodeConfig first — cheap and gives us dimensions + format.
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		common.ApiErrorMsg(c, "avatar is not a valid PNG or JPEG image")
		return
	}
	meta, ok := formatMeta[format]
	if !ok {
		// Rejects SVG / GIF / WEBP / HEIC even if a decoder is registered
		// elsewhere in the binary (e.g. via the service package).
		common.ApiErrorMsg(c, "avatar format not supported: only PNG and JPEG are allowed")
		return
	}

	if config.Width <= 0 || config.Height <= 0 {
		common.ApiErrorMsg(c, "avatar has invalid dimensions")
		return
	}
	if config.Width > avatarMaxDim || config.Height > avatarMaxDim {
		common.ApiErrorMsg(c, fmt.Sprintf("avatar dimensions too large: %dx%d (max %d per side)", config.Width, config.Height, avatarMaxDim))
		return
	}
	if int64(config.Width)*int64(config.Height) > avatarMaxPixels {
		common.ApiErrorMsg(c, fmt.Sprintf("avatar pixel count too large: %d (max %d)", config.Width*config.Height, avatarMaxPixels))
		return
	}

	// Full decode to guarantee the file is not truncated/corrupt —
	// DecodeConfig only reads headers.
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		common.ApiErrorMsg(c, "avatar image data is corrupt or truncated")
		return
	}

	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])

	avatarURL := fmt.Sprintf("/api/user/avatar/%d/%s.%s", userID, sha, meta.Ext)
	if err := model.StoreUserAvatar(userID, meta.ContentType, sha, data, avatarURL, avatarSourceUploaded); err != nil {
		common.SysLog(fmt.Sprintf("failed to store avatar for user %d: %v", userID, err))
		common.ApiErrorMsg(c, "failed to store avatar")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": uploadSelfAvatarResponse{
			AvatarURL:    avatarURL,
			AvatarSource: avatarSourceUploaded,
		},
	})
}

// rejectNonImageMagic inspects the leading bytes for signatures of payloads we
// never want to accept regardless of what image.DecodeConfig says. Returns a
// non-empty human-readable reason when the payload must be rejected.
func rejectNonImageMagic(data []byte) string {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	switch {
	case len(trimmed) == 0:
		return "avatar file is empty"
	case bytes.HasPrefix(trimmed, []byte("<svg")), bytes.HasPrefix(trimmed, []byte("<?xml")):
		return "avatar must not be SVG or XML"
	case bytes.HasPrefix(trimmed, []byte("<!")),
		bytes.HasPrefix(trimmed, []byte("<html")),
		bytes.HasPrefix(trimmed, []byte("<HTML")),
		bytes.HasPrefix(trimmed, []byte("<!DOCTYPE")):
		return "avatar must not be HTML"
	case bytes.HasPrefix(trimmed, []byte("data:")):
		return "avatar must not be a data URL"
	}
	return ""
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
