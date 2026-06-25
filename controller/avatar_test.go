package controller

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupAvatarTestDB mirrors the pattern in key_lookup_test.go: an in-memory
// SQLite DB, with the user + user_avatars tables migrated, and model.DB
// pointed at it for the duration of the test.
func setupAvatarTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainType := common.MainDatabaseType()
	originalLogType := common.LogDatabaseType()
	originalRedisEnabled := common.RedisEnabled

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	model.InitColumnNames()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserAvatar{}, &model.Log{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainType, originalLogType)
		common.RedisEnabled = originalRedisEnabled
		model.InitColumnNames()
	})

	return db
}

func seedAvatarUser(t *testing.T, db *gorm.DB, username string) *model.User {
	t.Helper()
	u := &model.User{
		Username: username,
		Password: "testpass123",
		Role:     common.RoleCommonUser,
		Group:    "default",
		AffCode:  username + "-aff",
	}
	require.NoError(t, db.Create(u).Error)
	return u
}

// validPNGBytes returns a small but syntactically valid PNG of the requested
// dimensions so the handler's image.DecodeConfig / image.Decode passes.
func validPNGBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 0, G: byte(x), B: byte(y), A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func validJPEGBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: byte(x), B: byte(y), A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}))
	return buf.Bytes()
}

// highEntropyPNGBytes fills the raw pixel buffer with a hash-keystream that
// PNG's deflate stage cannot compress, so the encoded file size scales
// linearly with pixel count. Used to produce a fixture that genuinely exceeds
// the avatar byte cap.
func highEntropyPNGBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// Generate a high-entropy byte stream by hashing a counter; copy it into
	// the RGB channels with alpha=255 so the image is fully opaque.
	need := w * h * 3
	stream := make([]byte, 0, need)
	counter := 0
	for len(stream) < need {
		h := sha256.Sum256([]byte{byte(counter), byte(counter >> 8), byte(counter >> 16)})
		stream = append(stream, h[:]...)
		counter++
	}
	si := 0
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = stream[si]
		img.Pix[i+1] = stream[si+1]
		img.Pix[i+2] = stream[si+2]
		img.Pix[i+3] = 255
		si += 3
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// buildMultipartAvatar builds a multipart/form-data body with a single file
// field named "avatar" carrying the supplied bytes / filename.
func buildMultipartAvatar(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(avatarFormField, filename)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return body, writer.FormDataContentType()
}

func newAvatarUploadContext(t *testing.T, userID int, body *bytes.Buffer, contentType string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/self/avatar", body)
	ctx.Request.Header.Set("Content-Type", contentType)
	ctx.Set("id", userID)
	return ctx, recorder
}

type avatarAPIResponse struct {
	Success bool                  `json:"success"`
	Message string                `json:"message"`
	Data    avatarAPIResponseData `json:"data"`
}

type avatarAPIResponseData struct {
	AvatarURL    string `json:"avatar_url"`
	AvatarSource string `json:"avatar_source"`
}

func decodeAvatarResponse(t *testing.T, recorder *httptest.ResponseRecorder) avatarAPIResponse {
	t.Helper()
	var resp avatarAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	return resp
}

func newDiscordAvatarSyncContext(t *testing.T, userID int, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/self/avatar/discord", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", userID)
	return ctx, recorder
}

// TestUploadAvatar_AcceptsValidPNG exercises the happy path for PNG upload and
// asserts the response contract: avatar_url + avatar_source="uploaded", and the
// user row is updated with the short fields (not the blob).
func TestUploadAvatar_AcceptsValidPNG(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "png-user")

	pngBytes := validPNGBytes(t, 8, 8)
	body, ct := buildMultipartAvatar(t, "avatar.png", pngBytes)
	ctx, recorder := newAvatarUploadContext(t, user.Id, body, ct)
	UploadSelfAvatar(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, "body: %s", recorder.Body.String())
	resp := decodeAvatarResponse(t, recorder)
	require.True(t, resp.Success, resp.Message)

	sum := sha256.Sum256(pngBytes)
	expected := fmt.Sprintf("/api/user/avatar/%d/%s.png", user.Id, hex.EncodeToString(sum[:]))
	assert.Equal(t, expected, resp.Data.AvatarURL)
	assert.Equal(t, "uploaded", resp.Data.AvatarSource)

	// users row carries the short fields only.
	var reloaded model.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, expected, reloaded.AvatarURL)
	assert.Equal(t, "uploaded", reloaded.AvatarSource)

	// blob row exists and matches.
	avatar, err := model.GetUserAvatarByUserAndHash(user.Id, hex.EncodeToString(sum[:]))
	require.NoError(t, err)
	assert.Equal(t, "image/png", avatar.ContentType)
	assert.Equal(t, pngBytes, avatar.Data)

	// response must not leak the raw bytes.
	assert.NotContains(t, recorder.Body.String(), "base64")
}

// TestUploadAvatar_AcceptsValidJPEG mirrors the PNG test for JPEG so a
// regression that only accepts one format is caught.
func TestUploadAvatar_AcceptsValidJPEG(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "jpeg-user")

	jpegBytes := validJPEGBytes(t, 8, 8)
	body, ct := buildMultipartAvatar(t, "avatar.jpg", jpegBytes)
	ctx, recorder := newAvatarUploadContext(t, user.Id, body, ct)
	UploadSelfAvatar(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, "body: %s", recorder.Body.String())
	resp := decodeAvatarResponse(t, recorder)
	require.True(t, resp.Success, resp.Message)

	sum := sha256.Sum256(jpegBytes)
	expected := fmt.Sprintf("/api/user/avatar/%d/%s.jpg", user.Id, hex.EncodeToString(sum[:]))
	assert.Equal(t, expected, resp.Data.AvatarURL)
	assert.Equal(t, "uploaded", resp.Data.AvatarSource)

	var reloaded model.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, expected, reloaded.AvatarURL)
}

// TestUploadAvatar_RejectsSVG verifies the SVG / XML rejection path. SVGs are
// a common XSS vector and must be rejected server-side even if the client lies
// about the Content-Type.
func TestUploadAvatar_RejectsSVG(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "svg-user")

	svgBytes := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect/></svg>`)
	body, ct := buildMultipartAvatar(t, "evil.svg", svgBytes)
	ctx, recorder := newAvatarUploadContext(t, user.Id, body, ct)
	UploadSelfAvatar(ctx)

	resp := decodeAvatarResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "SVG")

	// No blob row, no user row update.
	count := int64(0)
	require.NoError(t, db.Model(&model.UserAvatar{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.Zero(t, count)
	var reloaded model.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Empty(t, reloaded.AvatarURL)
}

// TestUploadAvatar_RejectsHTML guards the HTML rejection path. Browsers must
// never render an uploaded avatar as HTML.
func TestUploadAvatar_RejectsHTML(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "html-user")

	htmlBytes := []byte(`<!DOCTYPE html><html><body><script>alert(1)</script></body></html>`)
	body, ct := buildMultipartAvatar(t, "evil.html", htmlBytes)
	ctx, recorder := newAvatarUploadContext(t, user.Id, body, ct)
	UploadSelfAvatar(ctx)

	resp := decodeAvatarResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "HTML")
}

// TestUploadAvatar_RejectsOversize enforces the hard byte cap on the decoded
// file itself. A PNG that decodes larger than avatarMaxBytes is rejected even
// though it is a perfectly valid image.
func TestUploadAvatar_RejectsOversize(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "big-user")

	// Build a high-entropy PNG whose byte length exceeds the 512KB cap.
	pngBytes := highEntropyPNGBytes(t, 600, 600)
	if len(pngBytes) <= avatarMaxBytes {
		t.Fatalf("test fixture too small (%d bytes); increase dimensions to exceed %d", len(pngBytes), avatarMaxBytes)
	}
	body, ct := buildMultipartAvatar(t, "big.png", pngBytes)
	ctx, recorder := newAvatarUploadContext(t, user.Id, body, ct)
	UploadSelfAvatar(ctx)

	resp := decodeAvatarResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "too large")
}

// TestUploadAvatar_RejectsDataURL guards against clients that send a data:
// URL payload as the file contents instead of real image bytes.
func TestUploadAvatar_RejectsDataURL(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "data-user")

	dataURL := []byte("data:image/png;base64,iVBORw0KGgo=")
	body, ct := buildMultipartAvatar(t, "evil.png", dataURL)
	ctx, recorder := newAvatarUploadContext(t, user.Id, body, ct)
	UploadSelfAvatar(ctx)

	resp := decodeAvatarResponse(t, recorder)
	assert.False(t, resp.Success)
}

// TestGetUserAvatar_ServesBytesWithNosniff verifies the public read endpoint:
// returns the stored bytes with Content-Type, nosniff and immutable cache
// headers. Does not require authentication.
func TestGetUserAvatar_ServesBytesWithNosniff(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "read-user")

	pngBytes := validPNGBytes(t, 4, 4)
	sum := sha256.Sum256(pngBytes)
	sha := hex.EncodeToString(sum[:])
	require.NoError(t, model.UpsertUserAvatar(user.Id, "image/png", sha, pngBytes))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/user/avatar/%d/%s.png", user.Id, sha), nil)
	ctx.Params = gin.Params{
		{Key: "id", Value: fmt.Sprintf("%d", user.Id)},
		{Key: "hash", Value: sha + ".png"},
	}
	GetUserAvatar(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "image/png", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
	assert.Contains(t, recorder.Header().Get("Cache-Control"), "immutable")
	assert.Equal(t, pngBytes, recorder.Body.Bytes())
}

// TestGetUserAvatar_RejectsWrongHash verifies the stale/guessed URL contract: a
// caller without the current SHA256 cannot retrieve the bytes (404), even though
// a valid avatar exists for the user.
func TestGetUserAvatar_RejectsWrongHash(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "hash-user")

	pngBytes := validPNGBytes(t, 2, 2)
	sum := sha256.Sum256(pngBytes)
	sha := hex.EncodeToString(sum[:])
	require.NoError(t, model.UpsertUserAvatar(user.Id, "image/png", sha, pngBytes))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/user/avatar/%d/%s.png", user.Id, strings.Repeat("a", 64)), nil)
	ctx.Params = gin.Params{
		{Key: "id", Value: fmt.Sprintf("%d", user.Id)},
		{Key: "hash", Value: strings.Repeat("a", 64) + ".png"},
	}
	GetUserAvatar(ctx)

	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

// TestGetUserAvatar_RejectsMalformedHash guards the hash validation: a
// non-64-hex path segment is rejected rather than hitting the DB.
func TestGetUserAvatar_RejectsMalformedHash(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "malformed-user")

	for _, badHash := range []string{"not-a-hash.png", "xyz", strings.Repeat("g", 64)} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/user/avatar/%d/%s", user.Id, badHash), nil)
		ctx.Params = gin.Params{
			{Key: "id", Value: fmt.Sprintf("%d", user.Id)},
			{Key: "hash", Value: badHash},
		}
		GetUserAvatar(ctx)
		assert.Equal(t, http.StatusNotFound, recorder.Code, "expected 404 for hash %q", badHash)
	}
}

// TestDeleteSelfAvatar_ClearsFields verifies the DELETE endpoint removes the
// blob row and clears avatar_url / avatar_source on the users row so GetSelf /
// setupLogin start returning empty fields.
func TestDeleteSelfAvatar_ClearsFields(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "delete-user")

	pngBytes := validPNGBytes(t, 4, 4)
	sum := sha256.Sum256(pngBytes)
	sha := hex.EncodeToString(sum[:])
	require.NoError(t, model.UpsertUserAvatar(user.Id, "image/png", sha, pngBytes))
	require.NoError(t, model.SetUserAvatarFields(user.Id, "/api/user/avatar/"+fmt.Sprintf("%d", user.Id)+"/"+sha+".png", "uploaded"))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/user/self/avatar", nil)
	ctx.Set("id", user.Id)
	DeleteSelfAvatar(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	resp := decodeAvatarResponse(t, recorder)
	require.True(t, resp.Success, resp.Message)
	assert.Empty(t, resp.Data.AvatarURL)
	assert.Empty(t, resp.Data.AvatarSource)

	var reloaded model.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Empty(t, reloaded.AvatarURL)
	assert.Empty(t, reloaded.AvatarSource)

	count := int64(0)
	require.NoError(t, db.Model(&model.UserAvatar{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.Zero(t, count)
}

// TestDeleteSelfAvatar_IdempotentOnMissing verifies that deleting when no
// avatar exists is a no-op success (not an error).
func TestDeleteSelfAvatar_IdempotentOnMissing(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "delete-empty-user")

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/api/user/self/avatar", nil)
	ctx.Set("id", user.Id)
	DeleteSelfAvatar(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	resp := decodeAvatarResponse(t, recorder)
	require.True(t, resp.Success, resp.Message)
}

// TestUpdateSelf_IgnoresAvatarURL is the central contract test: PUT
// /api/user/self must NOT let a client write avatar_url / avatar_source
// directly. The cleanUser whitelist in UpdateSelf omits these fields, so a
// request body carrying them must leave the stored values untouched.
func TestUpdateSelf_IgnoresAvatarURL(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "whitelist-user")
	// Pre-set a real avatar so we can tell a write-through apart from a no-op.
	require.NoError(t, model.SetUserAvatarFields(user.Id, "/api/user/avatar/1/abc.png", "uploaded"))

	payload, err := common.Marshal(map[string]any{
		"username":      "whitelist-user",
		"display_name":  "Whitelist User",
		"avatar_url":    "/api/user/avatar/999/INJECTED.png",
		"avatar_source": "INJECTED",
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/user/self", bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", user.Id)
	UpdateSelf(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, "body: %s", recorder.Body.String())

	var reloaded model.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, "/api/user/avatar/1/abc.png", reloaded.AvatarURL, "avatar_url must not be writable via PUT /api/user/self")
	assert.Equal(t, "uploaded", reloaded.AvatarSource, "avatar_source must not be writable via PUT /api/user/self")
}

// TestGetSelf_ReturnsAvatarFields confirms the read path surfaces the new
// fields so the frontend can render them after login.
func TestGetSelf_ReturnsAvatarFields(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "getself-user")
	require.NoError(t, model.SetUserAvatarFields(user.Id, "/api/user/avatar/1/def.png", "uploaded"))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/self", nil)
	ctx.Set("id", user.Id)
	ctx.Set("role", user.Role)
	GetSelf(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			AvatarURL    string `json:"avatar_url"`
			AvatarSource string `json:"avatar_source"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	assert.Equal(t, "/api/user/avatar/1/def.png", resp.Data.AvatarURL)
	assert.Equal(t, "uploaded", resp.Data.AvatarSource)
}

// TestSetupLogin_ReturnsAvatarFields confirms setupLogin surfaces the new
// fields so post-login localStorage carries them. setupLogin writes to the
// session, so the handler must run behind a real sessions middleware.
func TestSetupLogin_ReturnsAvatarFields(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "login-user")
	require.NoError(t, model.SetUserAvatarFields(user.Id, "/api/user/avatar/1/ghi.png", "uploaded"))
	// Reload so the in-memory struct carries the updated fields before being
	// passed to setupLogin (SetUserAvatarFields only writes to the DB row).
	reloaded, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)
	user = reloaded

	r := gin.New()
	store := cookie.NewStore([]byte("avatar-test-secret"))
	r.Use(sessions.Sessions("session", store))
	r.POST("/api/user/login", func(c *gin.Context) {
		setupLogin(user, c)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/user/login", nil)
	r.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			AvatarURL    string `json:"avatar_url"`
			AvatarSource string `json:"avatar_source"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	assert.Equal(t, "/api/user/avatar/1/ghi.png", resp.Data.AvatarURL)
	assert.Equal(t, "uploaded", resp.Data.AvatarSource)
}

func TestSyncSelfDiscordAvatar_RequiresLogin(t *testing.T) {
	setupAvatarTestDB(t)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/self/avatar/discord", strings.NewReader(`{}`))

	SyncSelfDiscordAvatar(ctx)

	resp := decodeAvatarResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "authenticated")
}

func TestSyncSelfDiscordAvatar_UnboundUserReturnsBusinessError(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "discord-avatar-unbound")
	ctx, recorder := newDiscordAvatarSyncContext(t, user.Id, `{}`)

	SyncSelfDiscordAvatar(ctx)

	resp := decodeAvatarResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "Discord account is not bound")
}

func TestSyncSelfDiscordAvatar_ProtectedUploadedWithoutForce(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "discord-avatar-protected")
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"discord_id":            "123456789012345678",
		"discord_avatar_hash":   "protected_hash",
		"discord_refresh_token": "super-secret-token",
		"avatar_url":            "/api/user/avatar/1/uploaded.png",
		"avatar_source":         model.AvatarSourceUploaded,
	}).Error)
	ctx, recorder := newDiscordAvatarSyncContext(t, user.Id, `{"force":false}`)

	SyncSelfDiscordAvatar(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "protected_hash")
	assert.NotContains(t, recorder.Body.String(), "super-secret-token")
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Skipped      bool   `json:"skipped"`
			Reason       string `json:"reason"`
			AvatarURL    string `json:"avatar_url"`
			AvatarSource string `json:"avatar_source"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	assert.True(t, resp.Data.Skipped)
	assert.Equal(t, service.DiscordAvatarReasonUploadedProtected, resp.Data.Reason)
	assert.Equal(t, model.AvatarSourceUploaded, resp.Data.AvatarSource)
}

func TestSyncSelfDiscordAvatar_ForceOverwritesUploaded(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "discord-avatar-force")
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"discord_id":            "123456789012345678",
		"discord_avatar_hash":   "force_hash",
		"discord_refresh_token": "super-secret-token",
		"avatar_url":            "/api/user/avatar/1/uploaded.png",
		"avatar_source":         model.AvatarSourceUploaded,
	}).Error)
	withDiscordProfileAvatarServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(validPNGBytes(t, 4, 4))
	})
	ctx, recorder := newDiscordAvatarSyncContext(t, user.Id, `{"force":true}`)

	SyncSelfDiscordAvatar(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "force_hash")
	assert.NotContains(t, recorder.Body.String(), "super-secret-token")
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Synced       bool   `json:"synced"`
			Reason       string `json:"reason"`
			AvatarURL    string `json:"avatar_url"`
			AvatarSource string `json:"avatar_source"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	assert.True(t, resp.Data.Synced)
	assert.Equal(t, service.DiscordAvatarReasonStored, resp.Data.Reason)
	assert.Contains(t, resp.Data.AvatarURL, "/api/user/avatar/")
	assert.Equal(t, model.AvatarSourceDiscord, resp.Data.AvatarSource)

	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	assert.Equal(t, model.AvatarSourceDiscord, stored.AvatarSource)
	assert.Equal(t, resp.Data.AvatarURL, stored.AvatarURL)
}
