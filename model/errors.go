package model

import "errors"

// Common errors
var (
	ErrDatabase = errors.New("database error")
)

// User auth errors
var (
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrUserEmptyCredentials = errors.New("empty credentials")
)

// Token auth errors
var (
	ErrTokenNotProvided = errors.New("token not provided")
	ErrTokenInvalid     = errors.New("token invalid")
)

// Redemption errors
var ErrRedeemFailed = errors.New("redeem.failed")

// 2FA errors
var ErrTwoFANotEnabled = errors.New("2fa not enabled")

// Billing / quota errors
var (
	// ErrInsufficientUserQuota is returned by checked atomic quota-decrement
	// helpers (DecreaseUserQuotaIfEnough) when the user does not have enough
	// quota remaining to cover the requested amount. It lets the service
	// layer map the failure to ErrorCodeInsufficientUserQuota without
	// relying on string matching.
	ErrInsufficientUserQuota = errors.New("insufficient user quota")
)
