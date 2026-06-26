package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func truncateErrorLogs(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Exec("DELETE FROM error_logs").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM error_logs")
	})
}

func TestOldErrorLogCleanupPrimitives(t *testing.T) {
	truncateErrorLogs(t)
	ctx := context.Background()
	cutoff := int64(2000)

	for i, createdAt := range []int64{1000, 1500, 1999, 2000, 2500} {
		require.NoError(t, DB.WithContext(ctx).Create(&ErrorLog{
			CreatedAt:            createdAt,
			RequestId:            "req",
			NormalizedSignature:  "sig",
			OriginalErrorMessage: "message",
			UserId:               i + 1,
		}).Error)
	}

	count, err := CountOldErrorLogs(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)

	deleted, err := DeleteOldErrorLogsBatch(ctx, cutoff, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)

	count, err = CountOldErrorLogs(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	deleted, err = DeleteOldErrorLogsBatch(ctx, cutoff, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	var remaining []ErrorLog
	require.NoError(t, DB.WithContext(ctx).Order("created_at asc").Find(&remaining).Error)
	require.Len(t, remaining, 2)
	assert.Equal(t, []int64{2000, 2500}, []int64{remaining[0].CreatedAt, remaining[1].CreatedAt})
}
