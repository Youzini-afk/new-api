package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
)

// DisableAllUserTokens disables (status = TokenStatusDisabled) every non-deleted
// token owned by `userId`. It is a cross-DB-safe bulk update (GORM Updates),
// used by the local auto-ban path (BanUserAndDisableTokens) to converge token
// state when a user is banned. Returns gorm's RowsAffected-agnostic error.
func DisableAllUserTokens(userId int) error {
	if userId <= 0 {
		return errors.New("invalid user id")
	}
	result := DB.Model(&Token{}).
		Where("user_id = ?", userId).
		Update("status", common.TokenStatusDisabled)
	return result.Error
}

// GetUserRoleById returns the role for the given user id. Used by the relay
// sensitive-intercept to bypass admin/root users (they must not be blocked or
// auto-banned). Reads from the user cache path via GetUserById.
func GetUserRoleById(userId int) (int, error) {
	if userId <= 0 {
		return 0, errors.New("invalid user id")
	}
	user, err := GetUserById(userId, false)
	if err != nil {
		return 0, err
	}
	if user == nil {
		return 0, errors.New("user not found")
	}
	return user.Role, nil
}
