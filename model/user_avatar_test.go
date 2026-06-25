package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUserAvatarAutoMigrateAndCRUD exercises the separate-blob avatar table
// migration path and the round-trip of UpsertUserAvatar /
// GetUserAvatarByUserAndHash / SetUserAvatarFields / DeleteUserAvatar.
// Running under the package-level TestMain (task_cas_test.go) verifies the
// AutoMigrate list includes &UserAvatar{} — otherwise the table would not
// exist and the first query would error.
func TestUserAvatarAutoMigrateAndCRUD(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "avatar-user", Password: "x", Role: 1, AffCode: "av1"}
	require.NoError(t, DB.Create(user).Error)
	require.NotZero(t, user.Id)

	pngBytes := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0}
	sum := sha256.Sum256(pngBytes)
	sha := hex.EncodeToString(sum[:])

	// Upsert on a fresh row.
	require.NoError(t, UpsertUserAvatar(user.Id, "image/png", sha, pngBytes))

	// Read with matching hash.
	got, err := GetUserAvatarByUserAndHash(user.Id, sha)
	require.NoError(t, err)
	assert.Equal(t, "image/png", got.ContentType)
	assert.Equal(t, pngBytes, got.Data)
	assert.Equal(t, sha, got.SHA256)
	assert.Equal(t, len(pngBytes), got.Size)

	// Wrong hash must not match — old/guessed URL contract.
	_, err = GetUserAvatarByUserAndHash(user.Id, strings.Repeat("0", 64))
	assert.ErrorIs(t, err, ErrAvatarNotFound)

	// Re-upsert replaces the blob and the hash.
	newBytes := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3, 4, 5}
	newSum := sha256.Sum256(newBytes)
	newSha := hex.EncodeToString(newSum[:])
	require.NoError(t, UpsertUserAvatar(user.Id, "image/png", newSha, newBytes))

	// Old hash no longer resolves.
	_, err = GetUserAvatarByUserAndHash(user.Id, sha)
	assert.ErrorIs(t, err, ErrAvatarNotFound)
	// New hash resolves and data is the replacement.
	got, err = GetUserAvatarByUserAndHash(user.Id, newSha)
	require.NoError(t, err)
	assert.Equal(t, newBytes, got.Data)

	// SetUserAvatarFields writes the short fields on the users row.
	require.NoError(t, SetUserAvatarFields(user.Id, "/api/user/avatar/1/x.png", "uploaded"))
	var reloaded User
	require.NoError(t, DB.Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, "/api/user/avatar/1/x.png", reloaded.AvatarURL)
	assert.Equal(t, "uploaded", reloaded.AvatarSource)

	// SetUserAvatarFields must persist empty values (used by DELETE to clear).
	require.NoError(t, SetUserAvatarFields(user.Id, "", ""))
	require.NoError(t, DB.Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Empty(t, reloaded.AvatarURL)
	assert.Empty(t, reloaded.AvatarSource)

	// DeleteUserAvatar removes the row; a second delete is idempotent.
	require.NoError(t, DeleteUserAvatar(user.Id))
	_, err = GetUserAvatarByUserAndHash(user.Id, newSha)
	assert.ErrorIs(t, err, ErrAvatarNotFound)
	require.NoError(t, DeleteUserAvatar(user.Id))
}

// TestStoreUserAvatarRollsBackForMissingUser verifies StoreUserAvatar does not
// leave an orphan blob when the users row cannot be updated.
func TestStoreUserAvatarRollsBackForMissingUser(t *testing.T) {
	truncateTables(t)

	pngBytes := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 9}
	sum := sha256.Sum256(pngBytes)
	sha := hex.EncodeToString(sum[:])

	err := StoreUserAvatar(9999, "image/png", sha, pngBytes, "/api/user/avatar/9999/"+sha+".png", "uploaded")
	assert.Error(t, err)

	count := int64(0)
	require.NoError(t, DB.Model(&UserAvatar{}).Where("user_id = ?", 9999).Count(&count).Error)
	assert.Zero(t, count)
}

func TestStoreUserAvatarWithSourceGuard_AutoSyncSourceRules(t *testing.T) {
	for name, tc := range map[string]struct {
		initialSource string
		force         bool
		wantStored    bool
		wantErr       error
		wantSource    string
	}{
		"empty auto stores":      {initialSource: "", wantStored: true, wantSource: AvatarSourceDiscord},
		"discord auto stores":    {initialSource: AvatarSourceDiscord, wantStored: true, wantSource: AvatarSourceDiscord},
		"uploaded auto protects": {initialSource: AvatarSourceUploaded, wantErr: ErrAvatarSourceProtected, wantSource: AvatarSourceUploaded},
		"unknown auto protects":  {initialSource: "external", wantErr: ErrAvatarSourceProtected, wantSource: "external"},
		"force overwrites":       {initialSource: AvatarSourceUploaded, force: true, wantStored: true, wantSource: AvatarSourceDiscord},
	} {
		t.Run(name, func(t *testing.T) {
			truncateTables(t)
			user := &User{Username: "avatar-guard-" + strings.ReplaceAll(name, " ", "-"), Password: "x", Role: 1, AffCode: "guard-" + strings.ReplaceAll(name, " ", "-")}
			require.NoError(t, DB.Create(user).Error)
			if tc.initialSource != "" {
				require.NoError(t, SetUserAvatarFields(user.Id, "/api/user/avatar/old/hash.png", tc.initialSource))
			}

			pngBytes := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, byte(len(name))}
			sum := sha256.Sum256(pngBytes)
			sha := hex.EncodeToString(sum[:])
			stored, err := StoreUserAvatarWithSourceGuard(user.Id, "image/png", sha, pngBytes, "/api/user/avatar/1/"+sha+".png", AvatarSourceDiscord, tc.force)

			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantStored, stored)

			var reloaded User
			require.NoError(t, DB.Where("id = ?", user.Id).First(&reloaded).Error)
			assert.Equal(t, tc.wantSource, reloaded.AvatarSource)
			count := int64(0)
			require.NoError(t, DB.Model(&UserAvatar{}).Where("user_id = ?", user.Id).Count(&count).Error)
			if tc.wantStored {
				assert.Equal(t, int64(1), count)
				_, err = GetUserAvatarByUserAndHash(user.Id, sha)
				require.NoError(t, err)
			} else {
				assert.Zero(t, count, "protected guard must not leave an orphan/stale blob")
			}
		})
	}
}

func TestStoreUserAvatarWithSourceGuard_MissingUserDoesNotLeaveBlob(t *testing.T) {
	truncateTables(t)
	pngBytes := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 7}
	sum := sha256.Sum256(pngBytes)
	sha := hex.EncodeToString(sum[:])

	stored, err := StoreUserAvatarWithSourceGuard(12345, "image/png", sha, pngBytes, "/api/user/avatar/12345/"+sha+".png", AvatarSourceDiscord, false)
	assert.False(t, stored)
	assert.ErrorIs(t, err, ErrAvatarConditionalNoUser)

	count := int64(0)
	require.NoError(t, DB.Model(&UserAvatar{}).Where("user_id = ?", 12345).Count(&count).Error)
	assert.Zero(t, count)
}

// TestUserDeleteRemovesAvatar verifies account deletion removes the blob and
// makes old avatar URLs stop resolving.
func TestUserDeleteRemovesAvatar(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "avatar-delete", Password: "x", Role: 1, AffCode: "av-del"}
	require.NoError(t, DB.Create(user).Error)

	pngBytes := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 1}
	sum := sha256.Sum256(pngBytes)
	sha := hex.EncodeToString(sum[:])
	require.NoError(t, StoreUserAvatar(user.Id, "image/png", sha, pngBytes, "/api/user/avatar/test.png", "uploaded"))

	require.NoError(t, user.Delete())

	count := int64(0)
	require.NoError(t, DB.Model(&UserAvatar{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.Zero(t, count)
	_, err := GetUserAvatarByUserAndHash(user.Id, sha)
	assert.ErrorIs(t, err, ErrAvatarNotFound)
}
