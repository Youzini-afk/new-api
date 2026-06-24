package model

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// truncateScreeningTables wipes the Phase 5 tables for a clean test fixture.
func truncateScreeningTables(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Exec("DELETE FROM log_screening_records").Error)
	require.NoError(t, DB.Exec("DELETE FROM prompt_block_logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM ua_block_logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM suspicious_ip_marks").Error)
	require.NoError(t, DB.Unscoped().Exec("DELETE FROM users").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM log_screening_records")
		DB.Exec("DELETE FROM prompt_block_logs")
		DB.Exec("DELETE FROM ua_block_logs")
		DB.Exec("DELETE FROM suspicious_ip_marks")
		DB.Unscoped().Exec("DELETE FROM users")
	})
}

// TestLogScreeningRecord_AutoMigrateAndTableNames exercises AutoMigrate for the
// four Phase 5 models and asserts the dialect-safe physical column name for
// the reserved `window` field is in place (a non-reserved identifier).
func TestLogScreeningRecord_AutoMigrateAndTableNames(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(
		&LogScreeningRecord{},
		&PromptBlockLog{},
		&UABlockLog{},
		&SuspiciousIPMark{},
	))

	// The Window field must map to the non-reserved physical column window_label,
	// not the reserved identifier "window".
	columns, err := DB.Migrator().ColumnTypes(&LogScreeningRecord{})
	require.NoError(t, err)
	require.NotEmpty(t, columns)
	hasWindowLabel := false
	for _, c := range columns {
		name := c.Name()
		if name == "window_label" {
			hasWindowLabel = true
		}
		// The reserved identifier "window" must NOT be the physical column name.
		assert.NotEqual(t, "window", name, "reserved identifier must not be a physical column")
	}
	require.True(t, hasWindowLabel, "expected physical column window_label for the Window field")

	// Table names should be the GORM snake_case defaults.
	for _, table := range []string{
		"log_screening_records",
		"prompt_block_logs",
		"ua_block_logs",
		"suspicious_ip_marks",
	} {
		assert.True(t, DB.Migrator().HasTable(table), "expected table %q to exist", table)
	}
}

// TestLogScreeningRecord_NoBoolDefaultTags asserts that NO bool field on the
// Phase 5 models carries a gorm default tag. Per AGENTS.md, boolean defaults
// must be normalized in code (Go zero value + hooks) to avoid cross-dialect
// AutoMigrate churn.
func TestLogScreeningRecord_NoBoolDefaultTags(t *testing.T) {
	models := []interface{}{
		LogScreeningRecord{},
		PromptBlockLog{},
		UABlockLog{},
		SuspiciousIPMark{},
	}
	for _, m := range models {
		rt := reflect.TypeOf(m)
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.Type.Kind() != reflect.Bool {
				continue
			}
			tag := f.Tag.Get("gorm")
			assert.False(t,
				strings.Contains(tag, "default:"),
				"%s.%s bool field must not carry a gorm default tag (got %q)", rt.Name(), f.Name, tag,
			)
		}
	}
}

// TestUpsertLogScreeningRecord_InsertThenUpdate verifies the upsert path uses
// the dialect-safe window column (no PG-style quoting) and updates an existing
// row keyed by (user_id, rule_name, window, request_path).
func TestUpsertLogScreeningRecord_InsertThenUpdate(t *testing.T) {
	truncateScreeningTables(t)
	ctx := context.Background()

	rec := &LogScreeningRecord{
		UserId:       0, // user_id=0 is allowed at the model layer (admin-only data)
		Username:     "alice",
		RuleName:     "high_freq_1h",
		Window:       "1h",
		RequestPath:  "all",
		RequestCount: 10,
		RPM:          5,
	}
	created, err := UpsertLogScreeningRecord(ctx, rec)
	require.NoError(t, err)
	assert.True(t, created)

	// Second call with the same key updates rather than inserts.
	rec2 := &LogScreeningRecord{
		UserId:       rec.UserId,
		Username:     "alice2",
		RuleName:     rec.RuleName,
		Window:       rec.Window,
		RequestPath:  rec.RequestPath,
		RequestCount: 99,
	}
	created2, err := UpsertLogScreeningRecord(ctx, rec2)
	require.NoError(t, err)
	assert.False(t, created2, "second upsert with same key must update, not insert")

	var got LogScreeningRecord
	require.NoError(t, DB.WithContext(ctx).First(&got, "user_id = ? AND rule_name = ?", rec.UserId, rec.RuleName).Error)
	assert.Equal(t, 99, got.RequestCount)
	assert.Equal(t, "alice2", got.Username)
	assert.NotZero(t, got.Id)
}

// TestUpsertLogScreeningRecord_EmptyWindowMatches ensures empty-string window
// values are matched exactly (the raw WHERE clause uses the non-reserved
// column name and does not silently coerce empty values).
func TestUpsertLogScreeningRecord_EmptyWindowMatches(t *testing.T) {
	truncateScreeningTables(t)
	ctx := context.Background()

	rec := &LogScreeningRecord{
		UserId:      7,
		RuleName:    "rule_empty_window",
		Window:      "",
		RequestPath: "/v1/chat/completions",
	}
	created, err := UpsertLogScreeningRecord(ctx, rec)
	require.NoError(t, err)
	assert.True(t, created)

	// Different rule but same empty window — must insert as a separate row.
	rec2 := &LogScreeningRecord{
		UserId:      7,
		RuleName:    "rule_empty_window_2",
		Window:      "",
		RequestPath: "/v1/chat/completions",
	}
	created2, err := UpsertLogScreeningRecord(ctx, rec2)
	require.NoError(t, err)
	assert.True(t, created2, "different rule_name must insert a new row")

	// Same key as rec — must update, not insert.
	created3, err := UpsertLogScreeningRecord(ctx, &LogScreeningRecord{
		UserId:      7,
		RuleName:    "rule_empty_window",
		Window:      "",
		RequestPath: "/v1/chat/completions",
		RPM:         3,
	})
	require.NoError(t, err)
	assert.False(t, created3, "same key with empty window must update, not insert")

	var count int64
	require.NoError(t, DB.WithContext(ctx).Model(&LogScreeningRecord{}).Where("user_id = ?", 7).Count(&count).Error)
	assert.Equal(t, int64(2), count, "expected exactly two rows for user 7")
}

// TestUpsertLogScreeningRecord_ConcurrentNoDuplicates verifies concurrent
// upserts on the same key do not produce duplicate rows.
func TestUpsertLogScreeningRecord_ConcurrentNoDuplicates(t *testing.T) {
	truncateScreeningTables(t)
	ctx := context.Background()

	concurrency := 20
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			_, _ = UpsertLogScreeningRecord(ctx, &LogScreeningRecord{
				UserId:       77,
				Username:     "concurrent",
				RuleName:     "rule_c",
				Window:       "1h",
				RequestPath:  "all",
				RequestCount: 1,
			})
		}()
	}
	wg.Wait()

	// Exactly one row (no duplicates from the composite unique index).
	var count int64
	require.NoError(t, DB.Model(&LogScreeningRecord{}).
		Where("user_id = ? AND rule_name = ? AND "+logScreeningWindowColumn+" = ? AND request_path = ?",
			77, "rule_c", "1h", "all").
		Count(&count).Error)
	assert.Equal(t, int64(1), count, "concurrent upserts must not produce duplicate rows")
}

// TestDeleteExpiredLogScreeningRecords_Batched verifies expired records are
// removed and unexpired ones are kept, including the batch-limit clamp.
func TestDeleteExpiredLogScreeningRecords_Batched(t *testing.T) {
	truncateScreeningTables(t)
	ctx := context.Background()

	now := int64(2000000)
	// Insert 3 expired (expires_at < now) and 2 unexpired records.
	for i, exp := range []int64{now - 100, now - 10, now - 1, now + 100, 0} {
		require.NoError(t, DB.WithContext(ctx).Create(&LogScreeningRecord{
			UserId:     100 + i,
			RuleName:   "expire_rule",
			Window:     "1h",
			ExpiresAt:  exp, // 0 means "never expires"
			RequestPath: "all",
		}).Error)
	}

	deleted, err := DeleteExpiredLogScreeningRecords(ctx, now, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), deleted, "only the 3 expired rows should be deleted (expires_at>0 && <now)")

	var remaining int64
	require.NoError(t, DB.WithContext(ctx).Model(&LogScreeningRecord{}).Where("rule_name = ?", "expire_rule").Count(&remaining).Error)
	assert.Equal(t, int64(2), remaining, "the 2 unexpired rows (future + expires_at=0) must remain")

	// Limit clamp: a limit above the max (10000) is clamped down — behavior stays correct.
	_, err = DeleteExpiredLogScreeningRecords(ctx, now, 999999)
	require.NoError(t, err)
}

// TestCreatePromptBlockLog_AndUABlockLog_Normalization verifies the create
// helpers normalize trailing whitespace and clamp an out-of-range HTTP status.
func TestCreatePromptBlockLog_AndUABlockLog_Normalization(t *testing.T) {
	truncateScreeningTables(t)
	ctx := context.Background()

	require.NoError(t, CreatePromptBlockLog(ctx, &PromptBlockLog{
		UserId:         1,
		Username:       "  alice  ",
		Ip:             "1.2.3.4  ",
		RulePattern:    "  bad-pattern  ",
		HTTPStatusCode: 999, // out of range -> clamped to 400
		RequestPath:    "  /v1/chat  ",
		MatchMode:      " rule ",
	}))
	var pbl PromptBlockLog
	require.NoError(t, DB.WithContext(ctx).First(&pbl, "user_id = ?", 1).Error)
	assert.Equal(t, "alice", pbl.Username)
	assert.Equal(t, "1.2.3.4", pbl.Ip)
	assert.Equal(t, "bad-pattern", pbl.RulePattern)
	assert.Equal(t, 400, pbl.HTTPStatusCode, "out-of-range status must fall back to 400")
	assert.Equal(t, "/v1/chat", pbl.RequestPath)
	assert.Equal(t, "rule", pbl.MatchMode)
	assert.NotZero(t, pbl.CreatedAt)
	assert.Equal(t, pbl.CreatedAt, pbl.MatchedAt, "MatchedAt defaults to CreatedAt")

	require.NoError(t, CreateUABlockLog(ctx, &UABlockLog{
		UserId:         2,
		Username:       "bob",
		Ip:             "5.6.7.8",
		UserAgent:      "  curl/8.0  ",
		RulePattern:    "blocked-ua",
		HTTPStatusCode: 50, // < 100 -> clamped to 400
		RequestPath:    "/v1/messages",
		IsEmptyUA:     true,
	}))
	var ual UABlockLog
	require.NoError(t, DB.WithContext(ctx).First(&ual, "user_id = ?", 2).Error)
	assert.Equal(t, "curl/8.0", ual.UserAgent)
	assert.Equal(t, 400, ual.HTTPStatusCode)
	assert.True(t, ual.IsEmptyUA)
}

// TestSuspiciousIPMark_UpsertAndList verifies the suspicious-IP upsert
// increments the trigger count and the list helper aggregates by user.
func TestSuspiciousIPMark_UpsertAndList(t *testing.T) {
	truncateScreeningTables(t)
	ctx := context.Background()

	// First mark for (user=10, ip=1.1.1.1) -> created, trigger_count=1.
	created, err := UpsertSuspiciousIPMark(ctx, &SuspiciousIPMark{
		UserId:   10,
		Username: "alice",
		Ip:       "1.1.1.1",
		Source:   "log_screening",
	})
	require.NoError(t, err)
	assert.True(t, created)

	// Second mark for the same (user, ip) -> updated, trigger_count=2.
	created2, err := UpsertSuspiciousIPMark(ctx, &SuspiciousIPMark{
		UserId:   10,
		Username: "alice",
		Ip:       "1.1.1.1",
		Source:   "log_screening",
	})
	require.NoError(t, err)
	assert.False(t, created2)
	var mark SuspiciousIPMark
	require.NoError(t, DB.WithContext(ctx).First(&mark, "user_id = ?", 10).Error)
	assert.Equal(t, 2, mark.TriggerCount)

	// Validation errors.
	_, err = UpsertSuspiciousIPMark(ctx, &SuspiciousIPMark{UserId: 0, Ip: "1.1.1.1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid user_id")
	_, err = UpsertSuspiciousIPMark(ctx, &SuspiciousIPMark{UserId: 10, Ip: "  "})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ip is empty")

	// List aggregation groups marks per user, ordered by last_triggered_at desc.
	require.NoError(t, DB.WithContext(ctx).Create(&SuspiciousIPMark{
		UserId:   11,
		Username: "bob",
		Ip:       "2.2.2.2",
		Source:   "log_screening",
		LastTriggeredAt: 9000000,
	}).Error)
	result, err := ListSuspiciousIPMarksByUserIDs(ctx, []int{10, 11})
	require.NoError(t, err)
	require.Contains(t, result, 10)
	require.Contains(t, result, 11)
	assert.Len(t, result[10], 1)
	assert.Len(t, result[11], 1)
	assert.Equal(t, "1.1.1.1", result[10][0].Ip)

	// Empty input returns an empty (non-nil) map without hitting the DB.
	empty, err := ListSuspiciousIPMarksByUserIDs(ctx, nil)
	require.NoError(t, err)
	assert.NotNil(t, empty)
	assert.Empty(t, empty)
}

// TestSuspiciousIPMark_ConcurrentUpsertNoDuplicates verifies concurrent
// upserts on the same (user_id, ip) key do not produce duplicate rows and the
// trigger_count reflects the total number of hits (atomic increment).
func TestSuspiciousIPMark_ConcurrentUpsertNoDuplicates(t *testing.T) {
	truncateScreeningTables(t)
	ctx := context.Background()

	concurrency := 20
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			_, _ = UpsertSuspiciousIPMark(ctx, &SuspiciousIPMark{
				UserId:   42,
				Username: "concurrent",
				Ip:       "10.0.0.1",
				Source:   "test",
			})
		}()
	}
	wg.Wait()

	// Exactly one row (no duplicates).
	var count int64
	require.NoError(t, DB.Model(&SuspiciousIPMark{}).Where("user_id = ? AND ip = ?", 42, "10.0.0.1").Count(&count).Error)
	assert.Equal(t, int64(1), count, "concurrent upserts must not produce duplicate rows")

	// TriggerCount reflects all hits (atomic increment).
	var mark SuspiciousIPMark
	require.NoError(t, DB.First(&mark, "user_id = ? AND ip = ?", 42, "10.0.0.1").Error)
	assert.Equal(t, concurrency, mark.TriggerCount, "trigger_count must reflect all concurrent hits")
}
