package model

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createExternalAppTestUser(t *testing.T, quota int, status int) User {
	t.Helper()
	user := User{
		Username: fmt.Sprintf("external-user-%d", time.Now().UnixNano()),
		Password: "not-used-in-test",
		Quota:    quota,
		Status:   status,
		Group:    "default",
	}
	require.NoError(t, DB.Create(&user).Error)
	return user
}

func externalAppUserQuota(t *testing.T, userID int) int {
	t.Helper()
	var quota int
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userID).Pluck("quota", &quota).Error)
	return quota
}

func TestExternalAppAuthCodeIsSingleUse(t *testing.T) {
	truncateTables(t)
	user := createExternalAppTestUser(t, 1000, common.UserStatusEnabled)
	code, expiresAt, err := CreateExternalAppAuthCode("wtfib", user.Id, 2*time.Minute)
	require.NoError(t, err)
	assert.NotEmpty(t, code)
	assert.Greater(t, expiresAt, time.Now().Unix())

	resolved, err := ConsumeExternalAppAuthCode("wtfib", code)
	require.NoError(t, err)
	assert.Equal(t, user.Id, resolved.Id)

	_, err = ConsumeExternalAppAuthCode("wtfib", code)
	assert.ErrorIs(t, err, ErrExternalAuthCodeInvalid)
}

func TestExternalAppAuthCodeRejectsExpiredAndWrongApp(t *testing.T) {
	truncateTables(t)
	user := createExternalAppTestUser(t, 1000, common.UserStatusEnabled)
	code, _, err := CreateExternalAppAuthCode("wtfib", user.Id, time.Minute)
	require.NoError(t, err)

	_, err = ConsumeExternalAppAuthCode("other-app", code)
	assert.ErrorIs(t, err, ErrExternalAuthCodeInvalid)
	require.NoError(t, DB.Model(&ExternalAppAuthCode{}).Where("code_hash = ?", externalAuthCodeHash(code)).Update("expires_at", time.Now().Add(-time.Minute).Unix()).Error)
	_, err = ConsumeExternalAppAuthCode("wtfib", code)
	assert.ErrorIs(t, err, ErrExternalAuthCodeInvalid)
}

func TestExternalQuotaDebitIsIdempotent(t *testing.T) {
	truncateTables(t)
	user := createExternalAppTestUser(t, 1000, common.UserStatusEnabled)

	first, applied, err := ApplyExternalQuotaOperation("wtfib", "deposit-idempotent-001", user.Id, ExternalQuotaKindDebit, 300)
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, ExternalQuotaStatusCompleted, first.Status)
	assert.Equal(t, 700, first.QuotaAfter)

	replay, applied, err := ApplyExternalQuotaOperation("wtfib", "deposit-idempotent-001", user.Id, ExternalQuotaKindDebit, 300)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, first.Id, replay.Id)
	assert.Equal(t, 700, externalAppUserQuota(t, user.Id))

	var count int64
	require.NoError(t, DB.Model(&ExternalQuotaOperation{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestExternalQuotaOperationRejectsSemanticReuse(t *testing.T) {
	truncateTables(t)
	user := createExternalAppTestUser(t, 1000, common.UserStatusEnabled)
	_, _, err := ApplyExternalQuotaOperation("wtfib", "deposit-conflict-001", user.Id, ExternalQuotaKindDebit, 100)
	require.NoError(t, err)

	_, _, err = ApplyExternalQuotaOperation("wtfib", "deposit-conflict-001", user.Id, ExternalQuotaKindDebit, 101)
	assert.ErrorIs(t, err, ErrExternalOperationConflict)
	assert.Equal(t, 900, externalAppUserQuota(t, user.Id))
}

func TestExternalQuotaDebitFailureIsDurable(t *testing.T) {
	truncateTables(t)
	user := createExternalAppTestUser(t, 50, common.UserStatusEnabled)

	failed, applied, err := ApplyExternalQuotaOperation("wtfib", "deposit-insufficient-001", user.Id, ExternalQuotaKindDebit, 100)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, ExternalQuotaStatusFailed, failed.Status)
	assert.Equal(t, ExternalQuotaErrorInsufficient, failed.ErrorCode)
	assert.Equal(t, 50, failed.QuotaAfter)

	replay, applied, err := ApplyExternalQuotaOperation("wtfib", "deposit-insufficient-001", user.Id, ExternalQuotaKindDebit, 100)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, failed.Id, replay.Id)
	assert.Equal(t, 50, externalAppUserQuota(t, user.Id))
}

func TestExternalQuotaCreditWorksForDisabledUserAndIsIdempotent(t *testing.T) {
	truncateTables(t)
	user := createExternalAppTestUser(t, 25, common.UserStatusDisabled)

	operation, applied, err := ApplyExternalQuotaOperation("wtfib", "withdraw-credit-001", user.Id, ExternalQuotaKindCredit, 75)
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, ExternalQuotaStatusCompleted, operation.Status)
	assert.Equal(t, 100, operation.QuotaAfter)

	_, applied, err = ApplyExternalQuotaOperation("wtfib", "withdraw-credit-001", user.Id, ExternalQuotaKindCredit, 75)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, 100, externalAppUserQuota(t, user.Id))
}

func TestExternalQuotaSameOperationConcurrentOnlyAppliesOnce(t *testing.T) {
	truncateTables(t)
	user := createExternalAppTestUser(t, 1000, common.UserStatusEnabled)

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	appliedCount := 0
	var countMu sync.Mutex
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, applied, err := ApplyExternalQuotaOperation("wtfib", "deposit-concurrent-001", user.Id, ExternalQuotaKindDebit, 200)
			if err != nil {
				errs <- err
				return
			}
			if applied {
				countMu.Lock()
				appliedCount++
				countMu.Unlock()
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, 1, appliedCount)
	assert.Equal(t, 800, externalAppUserQuota(t, user.Id))

	operation, err := GetExternalQuotaOperation("wtfib", "deposit-concurrent-001")
	require.NoError(t, err)
	assert.Equal(t, ExternalQuotaStatusCompleted, operation.Status)
	_, err = GetExternalQuotaOperation("wtfib", "missing-operation")
	assert.True(t, errors.Is(err, ErrExternalOperationNotFound))
}
