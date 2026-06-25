package model

import (
	"errors"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertShortMsgTestUser creates a user with the given quota for the
// DecreaseUserQuotaIfEnough tests. Returns the inserted user id.
func insertShortMsgTestUser(t *testing.T, quota int) int {
	t.Helper()
	truncateTables(t)
	u := &User{
		Username: "shortmsg_user_" + common.GetRandomString(6),
		Password: "password1234",
		Quota:    quota,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  common.GetRandomString(8),
	}
	require.NoError(t, DB.Create(u).Error)
	return u.Id
}

// TestDecreaseUserQuotaIfEnough_Success verifies the happy path: a user with
// enough quota gets decremented atomically and the helper returns nil.
func TestDecreaseUserQuotaIfEnough_Success(t *testing.T) {
	uid := insertShortMsgTestUser(t, 1000)

	require.NoError(t, DecreaseUserQuotaIfEnough(uid, 300))

	var reloaded User
	require.NoError(t, DB.First(&reloaded, uid).Error)
	assert.Equal(t, 700, reloaded.Quota)
}

// TestDecreaseUserQuotaIfEnough_InsufficientQuota verifies the
// fail-closed contract: when the user has less than the requested amount,
// the helper returns ErrInsufficientUserQuota and the user's quota is
// left unchanged (the conditional WHERE clause matched zero rows).
func TestDecreaseUserQuotaIfEnough_InsufficientQuota(t *testing.T) {
	uid := insertShortMsgTestUser(t, 100)

	err := DecreaseUserQuotaIfEnough(uid, 200)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInsufficientUserQuota),
		"expected ErrInsufficientUserQuota, got %v", err)

	var reloaded User
	require.NoError(t, DB.First(&reloaded, uid).Error)
	assert.Equal(t, 100, reloaded.Quota, "quota must be unchanged on insufficient reserve")
}

// TestDecreaseUserQuotaIfEnough_ExactEqual verifies the >= boundary: a user
// with exactly the requested amount succeeds and leaves the user at 0
// (never negative).
func TestDecreaseUserQuotaIfEnough_ExactEqual(t *testing.T) {
	uid := insertShortMsgTestUser(t, 500)

	require.NoError(t, DecreaseUserQuotaIfEnough(uid, 500))

	var reloaded User
	require.NoError(t, DB.First(&reloaded, uid).Error)
	assert.Equal(t, 0, reloaded.Quota)
}

// TestDecreaseUserQuotaIfEnough_ZeroQuotaIsNoOp verifies quota==0 returns
// nil immediately without touching the DB.
func TestDecreaseUserQuotaIfEnough_ZeroQuotaIsNoOp(t *testing.T) {
	uid := insertShortMsgTestUser(t, 100)

	require.NoError(t, DecreaseUserQuotaIfEnough(uid, 0))

	var reloaded User
	require.NoError(t, DB.First(&reloaded, uid).Error)
	assert.Equal(t, 100, reloaded.Quota)
}

// TestDecreaseUserQuotaIfEnough_NegativeQuotaRejected verifies quota<0 is
// rejected (mirrors DecreaseUserQuota's guard).
func TestDecreaseUserQuotaIfEnough_NegativeQuotaRejected(t *testing.T) {
	uid := insertShortMsgTestUser(t, 100)

	err := DecreaseUserQuotaIfEnough(uid, -1)
	require.Error(t, err)

	var reloaded User
	require.NoError(t, DB.First(&reloaded, uid).Error)
	assert.Equal(t, 100, reloaded.Quota)
}

// TestDecreaseUserQuotaIfEnough_NonexistentUserReturnsInsufficient verifies
// that a non-existent user id returns ErrInsufficientUserQuota (RowsAffected
// == 0) rather than silently succeeding or returning a generic DB error.
func TestDecreaseUserQuotaIfEnough_NonexistentUserReturnsInsufficient(t *testing.T) {
	truncateTables(t)
	err := DecreaseUserQuotaIfEnough(999999, 100)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInsufficientUserQuota))
}

// TestDecreaseUserQuotaIfEnough_ConcurrentOnlyOneWins verifies the atomic
// reserve contract: two goroutines racing to reserve the same amount when
// the user only has enough for one must end with exactly one winner and the
// user's quota left at exactly the single-winner remainder (never negative).
func TestDecreaseUserQuotaIfEnough_ConcurrentOnlyOneWins(t *testing.T) {
	uid := insertShortMsgTestUser(t, 500)

	const goroutines = 4
	var (
		wg      sync.WaitGroup
		errs    = make([]error, goroutines)
		winners int
		mu      sync.Mutex
	)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			err := DecreaseUserQuotaIfEnough(uid, 500)
			errs[idx] = err
			if err == nil {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	require.Equal(t, 1, winners, "exactly one goroutine must win the atomic reserve")
	for i, err := range errs {
		if err == nil {
			continue
		}
		assert.True(t, errors.Is(err, ErrInsufficientUserQuota),
			"goroutine %d: expected ErrInsufficientUserQuota, got %v", i, err)
	}

	var reloaded User
	require.NoError(t, DB.First(&reloaded, uid).Error)
	assert.Equal(t, 0, reloaded.Quota, "user must end at exactly zero, never negative")
}
