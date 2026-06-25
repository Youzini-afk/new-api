package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discordAvatarPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: byte(20 * x), G: byte(20 * y), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func withDiscordAvatarServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	oldBase := DiscordAvatarCDNBaseURL
	oldClient := DiscordAvatarHTTPClient
	client := server.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return fmt.Errorf("redirect rejected")
	}
	DiscordAvatarCDNBaseURL = server.URL
	DiscordAvatarHTTPClient = client
	t.Cleanup(func() {
		server.Close()
		DiscordAvatarCDNBaseURL = oldBase
		DiscordAvatarHTTPClient = oldClient
	})
}

func truncateDiscordAvatarTables(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.Exec("DELETE FROM user_avatars").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM users").Error)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM user_avatars")
		model.DB.Exec("DELETE FROM users")
	})
}

func TestSyncDiscordAvatar_RejectsInvalidIDAndHash(t *testing.T) {
	validID := "123456789012345678"
	for name, user := range map[string]model.User{
		"missing id":    {Id: 1, DiscordAvatarHash: "hash"},
		"short id":      {Id: 1, DiscordId: "123456", DiscordAvatarHash: "hash"},
		"nonnumeric id": {Id: 1, DiscordId: "abc12345678901234", DiscordAvatarHash: "hash"},
		"missing hash":  {Id: 1, DiscordId: validID},
		"slash hash":    {Id: 1, DiscordId: validID, DiscordAvatarHash: "a/b"},
		"query hash":    {Id: 1, DiscordId: validID, DiscordAvatarHash: "abc?size=1"},
		"space hash":    {Id: 1, DiscordId: validID, DiscordAvatarHash: "abc def"},
		"hyphen hash":   {Id: 1, DiscordId: validID, DiscordAvatarHash: "abc-def"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := SyncDiscordAvatar(context.Background(), &user, false)
			require.Error(t, err)
			assert.True(t, result.Skipped)
			if strings.Contains(name, "id") || strings.Contains(name, "nonnumeric") {
				assert.ErrorIs(t, err, ErrDiscordAvatarMissingBinding)
				assert.Equal(t, DiscordAvatarReasonMissingBinding, result.Reason)
			} else {
				assert.ErrorIs(t, err, ErrDiscordAvatarMissingAvatar)
				assert.Equal(t, DiscordAvatarReasonMissingAvatar, result.Reason)
			}
		})
	}
}

func TestSyncDiscordAvatar_ImportsPNG(t *testing.T) {
	truncateDiscordAvatarTables(t)
	user := &model.User{Username: "discord-avatar-import", Password: "x", DiscordId: "123456789012345678", DiscordAvatarHash: "a_hash"}
	require.NoError(t, model.DB.Create(user).Error)
	pngBytes := discordAvatarPNG(t)
	withDiscordAvatarServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/avatars/123456789012345678/a_hash.png", r.URL.Path)
		assert.Equal(t, "128", r.URL.Query().Get("size"))
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(pngBytes)
	})

	result, err := SyncDiscordAvatar(context.Background(), user, false)
	require.NoError(t, err)
	assert.True(t, result.Synced)
	assert.Equal(t, DiscordAvatarReasonStored, result.Reason)
	assert.Equal(t, model.AvatarSourceDiscord, result.AvatarSource)
	assert.Contains(t, result.AvatarURL, fmt.Sprintf("/api/user/avatar/%d/", user.Id))

	var stored model.User
	require.NoError(t, model.DB.First(&stored, user.Id).Error)
	assert.Equal(t, model.AvatarSourceDiscord, stored.AvatarSource)
	assert.Equal(t, result.AvatarURL, stored.AvatarURL)
	sum := sha256.Sum256(pngBytes)
	_, err = model.GetUserAvatarByUserAndHash(user.Id, fmt.Sprintf("%x", sum[:]))
	require.NoError(t, err)
}

func TestSyncDiscordAvatar_DownloadAndImageFailures(t *testing.T) {
	for name, tc := range map[string]struct {
		handler http.HandlerFunc
		reason  string
		errIs   error
	}{
		"404": {handler: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }, reason: DiscordAvatarReasonDownloadFailed, errIs: ErrDiscordAvatarDownloadFailed},
		"429": {handler: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }, reason: DiscordAvatarReasonDownloadFailed, errIs: ErrDiscordAvatarDownloadFailed},
		"500": {handler: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }, reason: DiscordAvatarReasonDownloadFailed, errIs: ErrDiscordAvatarDownloadFailed},
		"invalid image": {handler: func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("not an image"))
		}, reason: DiscordAvatarReasonInvalidImage, errIs: ErrDiscordAvatarInvalidImage},
		"too large content length": {handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", AvatarMaxBytes+1))
			_, _ = w.Write([]byte("x"))
		}, reason: DiscordAvatarReasonDownloadFailed, errIs: ErrDiscordAvatarDownloadFailed},
		"too large body": {handler: func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(bytes.Repeat([]byte("x"), AvatarMaxBytes+1))
		}, reason: DiscordAvatarReasonDownloadFailed, errIs: ErrDiscordAvatarDownloadFailed},
	} {
		t.Run(name, func(t *testing.T) {
			truncateDiscordAvatarTables(t)
			user := &model.User{Username: "discord-avatar-failure-" + strings.ReplaceAll(name, " ", "-"), Password: "x", DiscordId: "123456789012345678", DiscordAvatarHash: "hash"}
			require.NoError(t, model.DB.Create(user).Error)
			withDiscordAvatarServer(t, tc.handler)

			result, err := SyncDiscordAvatar(context.Background(), user, false)
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.errIs)
			assert.True(t, result.Skipped)
			assert.Equal(t, tc.reason, result.Reason)
		})
	}
}

func TestSyncDiscordAvatar_RedirectRejected(t *testing.T) {
	truncateDiscordAvatarTables(t)
	user := &model.User{Username: "discord-avatar-redirect", Password: "x", DiscordId: "123456789012345678", DiscordAvatarHash: "hash"}
	require.NoError(t, model.DB.Create(user).Error)
	withDiscordAvatarServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/avatar.png", http.StatusFound)
	})

	result, err := SyncDiscordAvatar(context.Background(), user, false)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDiscordAvatarDownloadFailed)
	assert.Equal(t, DiscordAvatarReasonDownloadFailed, result.Reason)
}
