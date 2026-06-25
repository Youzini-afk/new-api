package model

import (
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	AvatarSourceUploaded = "uploaded"
	AvatarSourceDiscord  = "discord"
)

// UserAvatar stores the raw avatar bytes for a user. It is intentionally kept
// in a separate table so the users table only carries the short avatar_url /
// avatar_source fields — the binary blob never pollutes user rows or API
// responses that surface the user record.
//
// One row per user (unique user_id): the latest upload replaces any prior
// avatar for that user. The SHA256 doubles as a cache-busting token in the
// public read URL (/api/user/avatar/{user_id}/{sha}.{ext}) so stale URLs stop
// resolving when the avatar changes while still being cacheable forever.
type UserAvatar struct {
	ID          uint   `json:"id" gorm:"primaryKey"`
	UserID      int    `json:"user_id" gorm:"column:user_id;uniqueIndex"`
	ContentType string `json:"content_type" gorm:"column:content_type;type:varchar(32)"`
	Data        []byte `json:"-" gorm:"column:data"`
	SHA256      string `json:"sha256" gorm:"column:sha256;type:varchar(64)"`
	Size        int    `json:"size" gorm:"column:size"`
	CreatedAt   int64  `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	UpdatedAt   int64  `json:"updated_at" gorm:"autoUpdateTime;column:updated_at"`
}

func (UserAvatar) TableName() string {
	return "user_avatars"
}

// ErrAvatarNotFound is returned by GetAvatar when no row exists for the user.
var (
	ErrAvatarNotFound          = errors.New("avatar not found")
	ErrAvatarSourceProtected   = errors.New("avatar source protected")
	ErrAvatarConditionalNoUser = errors.New("user not found")
)

// UpsertUserAvatar stores (or replaces) the avatar blob for a user inside a
// single transaction. The unique index on user_id makes the OnConflict clause
// update the existing row in-place across SQLite / MySQL / PostgreSQL.
func UpsertUserAvatar(userID int, contentType, sha256 string, data []byte) error {
	if userID == 0 {
		return errors.New("user id is empty")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := ensureUserExistsWithDB(tx, userID); err != nil {
			return err
		}
		return upsertUserAvatarWithDB(tx, userID, contentType, sha256, data)
	})
}

func upsertUserAvatarWithDB(db *gorm.DB, userID int, contentType, sha256 string, data []byte) error {
	avatar := &UserAvatar{
		UserID:      userID,
		ContentType: contentType,
		Data:        data,
		SHA256:      sha256,
		Size:        len(data),
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"content_type",
			"data",
			"sha256",
			"size",
			"updated_at",
		}),
	}).Create(avatar).Error
}

// StoreUserAvatar atomically stores the blob and updates the users row to point
// at the immutable public URL. Keeping both writes in a transaction prevents a
// dangling blob or a users.avatar_url pointing at bytes that were not stored.
func StoreUserAvatar(userID int, contentType, sha256 string, data []byte, avatarURL, avatarSource string) error {
	_, err := StoreUserAvatarWithSourceGuard(userID, contentType, sha256, data, avatarURL, avatarSource, true)
	return err
}

// StoreUserAvatarWithSourceGuard atomically stores the blob and updates the
// users row while enforcing the Discord auto-sync source guard in the model
// layer. When force is false, only empty / discord avatar_source rows may be
// changed; uploaded or unknown non-empty sources are protected. The users row is
// conditionally updated before the blob upsert so a protected row cannot leave a
// new blob behind or invalidate an uploaded avatar's old hash URL.
func StoreUserAvatarWithSourceGuard(userID int, contentType, sha256 string, data []byte, avatarURL, avatarSource string, force bool) (bool, error) {
	if userID == 0 {
		return false, errors.New("user id is empty")
	}
	if strings.TrimSpace(avatarURL) == "" || strings.TrimSpace(avatarSource) == "" {
		return false, errors.New("avatar url and source are required")
	}
	stored := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&User{}).Where("id = ?", userID)
		if !force {
			query = query.Where("avatar_source = ? OR avatar_source = ? OR avatar_source IS NULL", "", AvatarSourceDiscord)
		}
		result := query.Updates(map[string]interface{}{
			"avatar_url":    avatarURL,
			"avatar_source": avatarSource,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var current User
			if err := tx.Select("id", "avatar_source").First(&current, "id = ?", userID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrAvatarConditionalNoUser
				}
				return err
			}
			if !force && !avatarSourceAllowsDiscordAutoSync(current.AvatarSource) {
				return ErrAvatarSourceProtected
			}
		}
		if err := upsertUserAvatarWithDB(tx, userID, contentType, sha256, data); err != nil {
			return err
		}
		stored = true
		return nil
	})
	return stored, err
}

func avatarSourceAllowsDiscordAutoSync(source string) bool {
	return source == "" || source == AvatarSourceDiscord
}

// GetUserAvatarByUserAndHash loads a user's avatar only when the stored SHA256
// matches the supplied hash. A caller without the current cache-busting hash
// cannot fetch the bytes through an old or guessed URL.
func GetUserAvatarByUserAndHash(userID int, hash string) (*UserAvatar, error) {
	if userID == 0 || hash == "" {
		return nil, ErrAvatarNotFound
	}
	if err := ensureUserExistsWithDB(DB, userID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAvatarNotFound
		}
		return nil, err
	}
	var avatar UserAvatar
	err := DB.Where("user_id = ? AND sha256 = ?", userID, hash).First(&avatar).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAvatarNotFound
		}
		return nil, err
	}
	return &avatar, nil
}

// DeleteUserAvatar removes the avatar blob row for a user. Missing rows are
// treated as success so the DELETE endpoint stays idempotent.
func DeleteUserAvatar(userID int) error {
	if userID == 0 {
		return errors.New("user id is empty")
	}
	return DB.Where("user_id = ?", userID).Delete(&UserAvatar{}).Error
}

// ClearUserAvatar atomically removes the blob row and clears users.avatar_url /
// users.avatar_source. Missing blob rows are treated as success so DELETE stays
// idempotent for callers.
func ClearUserAvatar(userID int) error {
	if userID == 0 {
		return errors.New("user id is empty")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&UserAvatar{}).Error; err != nil {
			return err
		}
		return setUserAvatarFieldsWithDB(tx, userID, "", "")
	})
}

// SetUserAvatarFields writes the avatar_url / avatar_source columns on the
// users row. Uses a map-based Updates so empty values (used by the DELETE
// endpoint to clear the fields) are persisted instead of being skipped by
// GORM's zero-value struct semantics.
func SetUserAvatarFields(userID int, avatarURL, avatarSource string) error {
	if userID == 0 {
		return errors.New("user id is empty")
	}
	return setUserAvatarFieldsWithDB(DB, userID, avatarURL, avatarSource)
}

func setUserAvatarFieldsWithDB(db *gorm.DB, userID int, avatarURL, avatarSource string) error {
	if err := ensureUserExistsWithDB(db, userID); err != nil {
		return err
	}
	return db.Model(&User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"avatar_url":    avatarURL,
		"avatar_source": avatarSource,
	}).Error
}

// DeleteUserAvatarWithDB removes avatar bytes for a user using the supplied
// transaction handle. Used by user deletion flows so an old public avatar URL
// stops resolving as soon as the account is removed.
func DeleteUserAvatarWithDB(db *gorm.DB, userID int) error {
	if userID == 0 {
		return errors.New("user id is empty")
	}
	return db.Where("user_id = ?", userID).Delete(&UserAvatar{}).Error
}

func ensureUserExistsWithDB(db *gorm.DB, userID int) error {
	if userID == 0 {
		return errors.New("user id is empty")
	}
	var user User
	return db.Select("id").First(&user, "id = ?", userID).Error
}
