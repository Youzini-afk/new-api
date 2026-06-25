package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
)

const (
	// AvatarMaxBytes is the hard cap on the decoded avatar file itself.
	AvatarMaxBytes = 512 * 1024 // 512 KiB

	// AvatarMaxBodyBytes bounds multipart avatar upload request bodies.
	AvatarMaxBodyBytes = 5 * 1024 * 1024 // 5 MiB

	// AvatarMaxDim caps width / height in pixels.
	AvatarMaxDim = 2048

	// AvatarMaxPixels caps total pixels (width * height) to bound decode cost.
	AvatarMaxPixels = 4 * 1024 * 1024 // 4 megapixels

	// AvatarFormField is the multipart form field name clients must use.
	AvatarFormField = "avatar"
)

// AvatarValidationError marks errors caused by untrusted image payloads rather
// than storage/database failures.
type AvatarValidationError struct {
	Message string
}

func (e *AvatarValidationError) Error() string {
	return e.Message
}

// AvatarImage is the normalized, verified image payload ready for DB storage.
type AvatarImage struct {
	ContentType string
	Ext         string
	SHA256      string
	AvatarURL   string
	Data        []byte
}

// formatMeta maps an image.DecodeConfig format string to its content-type and
// URL extension. Only entries here are accepted.
var avatarFormatMeta = map[string]struct {
	ContentType string
	Ext         string
}{
	"png":  {"image/png", "png"},
	"jpeg": {"image/jpeg", "jpg"},
}

// ValidateAvatarImage verifies uploaded/imported avatar bytes without trusting
// MIME headers or filenames. It rejects SVG / GIF / HTML / data URLs, oversized
// payloads, excessive dimensions/pixels and corrupt/truncated images.
func ValidateAvatarImage(userID int, data []byte) (AvatarImage, error) {
	if len(data) == 0 {
		return AvatarImage{}, avatarValidationError("avatar file is empty")
	}
	if len(data) > AvatarMaxBytes {
		return AvatarImage{}, avatarValidationError(fmt.Sprintf("avatar file too large (max %d bytes)", AvatarMaxBytes))
	}
	if msg := rejectNonImageMagic(data); msg != "" {
		return AvatarImage{}, avatarValidationError(msg)
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return AvatarImage{}, avatarValidationError("avatar is not a valid PNG or JPEG image")
	}
	meta, ok := avatarFormatMeta[format]
	if !ok {
		return AvatarImage{}, avatarValidationError("avatar format not supported: only PNG and JPEG are allowed")
	}
	if config.Width <= 0 || config.Height <= 0 {
		return AvatarImage{}, avatarValidationError("avatar has invalid dimensions")
	}
	if config.Width > AvatarMaxDim || config.Height > AvatarMaxDim {
		return AvatarImage{}, avatarValidationError(fmt.Sprintf("avatar dimensions too large: %dx%d (max %d per side)", config.Width, config.Height, AvatarMaxDim))
	}
	if int64(config.Width)*int64(config.Height) > AvatarMaxPixels {
		return AvatarImage{}, avatarValidationError(fmt.Sprintf("avatar pixel count too large: %d (max %d)", config.Width*config.Height, AvatarMaxPixels))
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return AvatarImage{}, avatarValidationError("avatar image data is corrupt or truncated")
	}

	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	return AvatarImage{
		ContentType: meta.ContentType,
		Ext:         meta.Ext,
		SHA256:      sha,
		AvatarURL:   BuildUserAvatarURL(userID, sha, meta.Ext),
		Data:        data,
	}, nil
}

func avatarValidationError(message string) error {
	return &AvatarValidationError{Message: message}
}

// BuildUserAvatarURL returns the local immutable avatar URL. Callers must never
// persist a remote CDN URL into users.avatar_url.
func BuildUserAvatarURL(userID int, sha, ext string) string {
	return fmt.Sprintf("/api/user/avatar/%d/%s.%s", userID, sha, ext)
}

// rejectNonImageMagic inspects leading bytes for payloads we never accept.
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
