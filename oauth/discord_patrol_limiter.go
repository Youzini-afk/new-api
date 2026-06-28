package oauth

import (
	"context"
	"sync"
	"time"
)

type discordPatrolLimiterContextKey struct{}

type discordPatrolLimiter struct {
	mu          sync.Mutex
	minInterval time.Duration
	nextAllowed time.Time
	pauseUntil  time.Time
}

func NewDiscordPatrolLimiter(maxRPS int) *discordPatrolLimiter {
	if maxRPS < 1 {
		maxRPS = 1
	}
	return &discordPatrolLimiter{minInterval: time.Second / time.Duration(maxRPS)}
}

func ContextWithDiscordPatrolLimiter(ctx context.Context, limiter *discordPatrolLimiter) context.Context {
	if limiter == nil {
		return ctx
	}
	return context.WithValue(ctx, discordPatrolLimiterContextKey{}, limiter)
}

func discordPatrolLimiterFromContext(ctx context.Context) *discordPatrolLimiter {
	if ctx == nil {
		return nil
	}
	limiter, _ := ctx.Value(discordPatrolLimiterContextKey{}).(*discordPatrolLimiter)
	return limiter
}

func waitDiscordPatrolLimiter(ctx context.Context) error {
	limiter := discordPatrolLimiterFromContext(ctx)
	if limiter == nil {
		return nil
	}
	for {
		limiter.mu.Lock()
		now := time.Now()
		waitUntil := limiter.nextAllowed
		if limiter.pauseUntil.After(waitUntil) {
			waitUntil = limiter.pauseUntil
		}
		if !waitUntil.After(now) {
			limiter.nextAllowed = now.Add(limiter.minInterval)
			limiter.mu.Unlock()
			return nil
		}
		wait := waitUntil.Sub(now)
		limiter.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func recordDiscordPatrolDiagnostic(ctx context.Context, diag discordDiagnostic) {
	if diag.Category != discordCategoryRateLimited || diag.RetryAfter <= 0 {
		return
	}
	limiter := discordPatrolLimiterFromContext(ctx)
	if limiter == nil {
		return
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	pauseUntil := time.Now().Add(diag.RetryAfter)
	if pauseUntil.After(limiter.pauseUntil) {
		limiter.pauseUntil = pauseUntil
	}
}
