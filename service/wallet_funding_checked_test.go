package service

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWalletFunding_CheckedPreConsume_UsesAtomicHelperAndFailsOnInsufficient
// verifies the Phase 10B contract: when RequireCheckedPreConsume is set on
// relayInfo (set by the enforce-mode preflight), the wallet funding uses
// DecreaseUserQuotaIfEnough's atomic conditional update, so an
// insufficient-quota user fails cleanly with ErrInsufficientUserQuota
// mapped to ErrorCodeInsufficientUserQuota — never an overdraft.
func TestWalletFunding_CheckedPreConsume_UsesAtomicHelperAndFailsOnInsufficient(t *testing.T) {
	gin.SetMode(gin.TestMode)

	truncate(t)
	const userID = 7001
	seedUser(t, userID, 100) // 100 quota, less than the 500 preconsume
	const tokenID = 8001
	seedToken(t, tokenID, userID, "sk-checked-insufficient", 10000)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		UserId:                   userID,
		TokenId:                  tokenID,
		TokenKey:                 "sk-checked-insufficient",
		OriginModelName:          "gpt-4o-mini",
		ForcePreConsume:          true,
		RequireCheckedPreConsume: true,
	}

	apiErr := PreConsumeBilling(ctx, 500, relayInfo)
	require.NotNil(t, apiErr, "checked preconsume must fail with insufficient quota")
	require.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	require.Equal(t, 403, apiErr.StatusCode)

	// User quota must be unchanged (the conditional WHERE matched zero rows).
	quota, err := model.GetUserQuota(userID, true)
	require.NoError(t, err)
	assert.Equal(t, 100, quota, "insufficient reserve must not modify quota")

	// Billing must not have been assigned (PreConsumeBilling returns before
	// setting relayInfo.Billing on error).
	assert.Nil(t, relayInfo.Billing)
}

// TestWalletFunding_CheckedPreConsume_SucceedsOnSufficientQuota verifies the
// happy path: a user with enough quota gets the atomic reserve applied and
// relayInfo.Billing is created with the expected preconsumed amount.
func TestWalletFunding_CheckedPreConsume_SucceedsOnSufficientQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)

	truncate(t)
	const userID = 7002
	seedUser(t, userID, 1000) // 1000 quota, more than the 500 preconsume
	const tokenID = 8002
	seedToken(t, tokenID, userID, "sk-checked-success", 10000)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		UserId:                   userID,
		TokenId:                  tokenID,
		TokenKey:                 "sk-checked-success",
		OriginModelName:          "gpt-4o-mini",
		ForcePreConsume:          true,
		RequireCheckedPreConsume: true,
	}

	apiErr := PreConsumeBilling(ctx, 500, relayInfo)
	require.Nil(t, apiErr)

	require.NotNil(t, relayInfo.Billing)
	assert.Equal(t, 500, relayInfo.Billing.GetPreConsumedQuota())
	assert.Equal(t, BillingSourceWallet, relayInfo.BillingSource)

	quota, err := model.GetUserQuota(userID, true)
	require.NoError(t, err)
	assert.Equal(t, 500, quota, "atomic reserve must decrement quota by 500")
}

// TestWalletFunding_CheckedPreConsume_DisabledUsesNonAtomicPath verifies the
// default non-enforce path: when RequireCheckedPreConsume is false, the
// wallet uses the legacy DecreaseUserQuota path (which may allow trust
// bypass / BatchUpdate). This locks that enforce does not perturb normal
// preconsume behavior.
func TestWalletFunding_CheckedPreConsume_DisabledUsesNonAtomicPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	truncate(t)
	const userID = 7003
	seedUser(t, userID, 1000)
	const tokenID = 8003
	seedToken(t, tokenID, userID, "sk-checked-disabled", 10000)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        "sk-checked-disabled",
		OriginModelName: "gpt-4o-mini",
		ForcePreConsume: true,
		// RequireCheckedPreConsume defaults to false.
	}

	apiErr := PreConsumeBilling(ctx, 500, relayInfo)
	require.Nil(t, apiErr)

	require.NotNil(t, relayInfo.Billing)
	assert.Equal(t, 500, relayInfo.Billing.GetPreConsumedQuota())

	quota, err := model.GetUserQuota(userID, true)
	require.NoError(t, err)
	assert.Equal(t, 500, quota)
}

// TestDecreaseUserQuotaIfEnough_ErrIsSentinelForErrorsIs verifies the
// sentinel is recognized through errors.Is (used by the service layer's
// errors.Is(err, model.ErrInsufficientUserQuota) check).
func TestDecreaseUserQuotaIfEnough_ErrIsSentinelForErrorsIs(t *testing.T) {
	truncate(t)
	const userID = 7004
	seedUser(t, userID, 50)

	err := model.DecreaseUserQuotaIfEnough(userID, 100)
	require.Error(t, err)
	require.True(t, errors.Is(err, model.ErrInsufficientUserQuota))
}
