package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
)

const (
	DiscordAvatarReasonMissingBinding    = "missing_discord_binding"
	DiscordAvatarReasonMissingAvatar     = "missing_discord_avatar"
	DiscordAvatarReasonUploadedProtected = "uploaded_avatar_protected"
	DiscordAvatarReasonUnchanged         = "unchanged"
	DiscordAvatarReasonDownloadFailed    = "download_failed"
	DiscordAvatarReasonInvalidImage      = "invalid_image"
	DiscordAvatarReasonStored            = "stored"

	discordSnowflakeMinLen     = 17
	discordSnowflakeMaxLen     = 20
	discordAvatarHashMaxLength = 128
)

var (
	ErrDiscordAvatarMissingBinding = errors.New("missing Discord binding")
	ErrDiscordAvatarMissingAvatar  = errors.New("missing Discord avatar")
	ErrDiscordAvatarDownloadFailed = errors.New("Discord avatar download failed")
	ErrDiscordAvatarInvalidImage   = errors.New("Discord avatar image is invalid")

	DiscordAvatarCDNBaseURL = "https://cdn.discordapp.com"
	DiscordAvatarHTTPClient = newDiscordAvatarHTTPClient()
)

// DiscordAvatarSyncResult is safe to return to clients. It intentionally omits
// refresh tokens, Discord avatar hashes and any remote CDN URL.
type DiscordAvatarSyncResult struct {
	Synced       bool   `json:"synced"`
	Skipped      bool   `json:"skipped"`
	Reason       string `json:"reason"`
	AvatarURL    string `json:"avatar_url"`
	AvatarSource string `json:"avatar_source"`
}

// SyncDiscordAvatar imports the user's current Discord avatar into the local
// DB-backed avatar store. Automatic callers must pass force=false so uploaded
// or unknown non-empty avatar_source rows are protected by the model guard.
func SyncDiscordAvatar(ctx context.Context, user *model.User, force bool) (DiscordAvatarSyncResult, error) {
	result := discordAvatarResultFromUser(user)
	if user == nil || user.Id == 0 || !isValidDiscordSnowflake(user.DiscordId) {
		result.Skipped = true
		result.Reason = DiscordAvatarReasonMissingBinding
		return result, ErrDiscordAvatarMissingBinding
	}
	avatarHash := strings.TrimSpace(user.DiscordAvatarHash)
	if avatarHash == "" {
		result.Skipped = true
		result.Reason = DiscordAvatarReasonMissingAvatar
		return result, ErrDiscordAvatarMissingAvatar
	}
	if !isValidDiscordAvatarHash(avatarHash) {
		result.Skipped = true
		result.Reason = DiscordAvatarReasonMissingAvatar
		return result, ErrDiscordAvatarMissingAvatar
	}
	if !force && !discordAutoSyncSourceAllowed(user.AvatarSource) {
		result.Skipped = true
		result.Reason = DiscordAvatarReasonUploadedProtected
		return result, nil
	}

	data, err := downloadDiscordAvatar(ctx, strings.TrimSpace(user.DiscordId), avatarHash)
	if err != nil {
		result.Skipped = true
		result.Reason = DiscordAvatarReasonDownloadFailed
		return result, fmt.Errorf("%w: %v", ErrDiscordAvatarDownloadFailed, err)
	}
	avatar, err := ValidateAvatarImage(user.Id, data)
	if err != nil {
		result.Skipped = true
		result.Reason = DiscordAvatarReasonInvalidImage
		return result, fmt.Errorf("%w: %v", ErrDiscordAvatarInvalidImage, err)
	}
	if strings.TrimSpace(user.AvatarSource) == model.AvatarSourceDiscord && user.AvatarURL == avatar.AvatarURL {
		result.Skipped = true
		result.Reason = DiscordAvatarReasonUnchanged
		result.AvatarURL = user.AvatarURL
		result.AvatarSource = user.AvatarSource
		return result, nil
	}

	stored, err := model.StoreUserAvatarWithSourceGuard(user.Id, avatar.ContentType, avatar.SHA256, avatar.Data, avatar.AvatarURL, model.AvatarSourceDiscord, force)
	if err != nil {
		if errors.Is(err, model.ErrAvatarSourceProtected) {
			result.Skipped = true
			result.Reason = DiscordAvatarReasonUploadedProtected
			return result, nil
		}
		if errors.Is(err, model.ErrAvatarConditionalNoUser) {
			result.Skipped = true
			result.Reason = DiscordAvatarReasonMissingBinding
			return result, ErrDiscordAvatarMissingBinding
		}
		return result, err
	}
	if !stored {
		result.Skipped = true
		result.Reason = DiscordAvatarReasonUploadedProtected
		return result, nil
	}
	result.Synced = true
	result.Reason = DiscordAvatarReasonStored
	result.AvatarURL = avatar.AvatarURL
	result.AvatarSource = model.AvatarSourceDiscord
	user.AvatarURL = avatar.AvatarURL
	user.AvatarSource = model.AvatarSourceDiscord
	return result, nil
}

func discordAvatarResultFromUser(user *model.User) DiscordAvatarSyncResult {
	if user == nil {
		return DiscordAvatarSyncResult{}
	}
	return DiscordAvatarSyncResult{
		AvatarURL:    user.AvatarURL,
		AvatarSource: user.AvatarSource,
	}
}

func discordAutoSyncSourceAllowed(source string) bool {
	source = strings.TrimSpace(source)
	return source == "" || source == model.AvatarSourceDiscord
}

func downloadDiscordAvatar(ctx context.Context, discordID, avatarHash string) ([]byte, error) {
	endpoint := buildDiscordAvatarURL(discordID, avatarHash)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "image/png,image/jpeg;q=0.9,*/*;q=0.1")

	client := DiscordAvatarHTTPClient
	if client == nil {
		client = newDiscordAvatarHTTPClient()
	}
	res, err := client.Do(req)
	if err != nil {
		if res != nil && res.Body != nil {
			_ = res.Body.Close()
		}
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status=%d", res.StatusCode)
	}
	if res.ContentLength > AvatarMaxBytes {
		return nil, fmt.Errorf("content-length %d exceeds max %d", res.ContentLength, AvatarMaxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, AvatarMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > AvatarMaxBytes {
		return nil, fmt.Errorf("body exceeds max %d", AvatarMaxBytes)
	}
	return data, nil
}

func buildDiscordAvatarURL(discordID, avatarHash string) string {
	base := strings.TrimRight(DiscordAvatarCDNBaseURL, "/")
	return base + "/avatars/" + url.PathEscape(discordID) + "/" + url.PathEscape(avatarHash) + ".png?size=128"
}

func isValidDiscordSnowflake(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < discordSnowflakeMinLen || len(value) > discordSnowflakeMaxLen {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isValidDiscordAvatarHash(value string) bool {
	if value == "" || len(value) > discordAvatarHashMaxLength || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func newDiscordAvatarHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 3 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return errors.New("discord avatar redirect rejected")
		},
	}
}
