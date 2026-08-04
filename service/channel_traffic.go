package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

const (
	channelTrafficContextKey       = "channel_traffic_admissions"
	channelTrafficRedisPrefix      = "channel:traffic:"
	channelTrafficLeaseDuration    = 90 * time.Second
	channelTrafficHeartbeat        = 30 * time.Second
	channelTrafficConcurrencyRetry = 100 * time.Millisecond
)

const (
	channelTrafficAcquired = 1
	channelTrafficQueued   = 2
	channelTrafficFull     = 3
)

var channelTrafficAdmissionScript = redis.NewScript(`
local queue_key = KEYS[1]
local active_key = KEYS[2]
local rpm_key = KEYS[3]
local sequence_key = KEYS[4]

local token = ARGV[1]
local max_concurrency = tonumber(ARGV[2])
local max_rpm = tonumber(ARGV[3])
local queue_size = tonumber(ARGV[4])
local queue_timeout_ms = tonumber(ARGV[5])
local lease_ms = tonumber(ARGV[6])

local redis_time = redis.call('TIME')
local now_ms = tonumber(redis_time[1]) * 1000 + math.floor(tonumber(redis_time[2]) / 1000)

redis.call('ZREMRANGEBYSCORE', active_key, '-inf', now_ms)
redis.call('ZREMRANGEBYSCORE', rpm_key, '-inf', now_ms - 60000)
if queue_timeout_ms > 0 then
  local queue_cutoff = (now_ms - queue_timeout_ms) * 1000 + 999
  redis.call('ZREMRANGEBYSCORE', queue_key, '-inf', queue_cutoff)
else
  redis.call('DEL', queue_key)
end

local active_count = redis.call('ZCARD', active_key)
local rpm_count = redis.call('ZCARD', rpm_key)

local function concurrency_available()
  return max_concurrency <= 0 or active_count < max_concurrency
end

local function rpm_available()
  return max_rpm <= 0 or rpm_count < max_rpm
end

local function refresh_expiry()
  local queue_ttl = math.max(queue_timeout_ms * 2, 120000)
  redis.call('PEXPIRE', queue_key, queue_ttl)
  redis.call('PEXPIRE', sequence_key, queue_ttl)
  redis.call('PEXPIRE', active_key, lease_ms * 2)
  redis.call('PEXPIRE', rpm_key, 120000)
end

local function admit()
  redis.call('ZREM', queue_key, token)
  if max_concurrency > 0 then
    redis.call('ZADD', active_key, now_ms + lease_ms, token)
    active_count = active_count + 1
  end
  if max_rpm > 0 then
    redis.call('ZADD', rpm_key, now_ms, token)
    rpm_count = rpm_count + 1
  end
  refresh_expiry()
  return {1, 0, active_count, 0, rpm_count}
end

local queue_score = redis.call('ZSCORE', queue_key, token)
local queue_count = redis.call('ZCARD', queue_key)
if not queue_score then
  if queue_count == 0 and concurrency_available() and rpm_available() then
    return admit()
  end
  if queue_size <= 0 or queue_count >= queue_size then
    refresh_expiry()
    return {3, 0, active_count, queue_count, rpm_count}
  end
  local sequence = redis.call('INCR', sequence_key)
  queue_score = now_ms * 1000 + (sequence % 1000)
  redis.call('ZADD', queue_key, queue_score, token)
  queue_count = queue_count + 1
end

local first = redis.call('ZRANGE', queue_key, 0, 0)[1]
if first == token and concurrency_available() and rpm_available() then
  return admit()
end

local retry_ms = 100
if not rpm_available() and max_rpm > 0 then
  local oldest = redis.call('ZRANGE', rpm_key, 0, 0, 'WITHSCORES')
  if oldest[2] then
    retry_ms = math.max(retry_ms, tonumber(oldest[2]) + 60000 - now_ms)
  end
end
local rank = redis.call('ZRANK', queue_key, token)
local position = 1
if rank then
  position = rank + 1
end
retry_ms = math.max(retry_ms, math.min(5000, position * 50))
refresh_expiry()
return {2, retry_ms, active_count, position, rpm_count}
`)

var channelTrafficRenewScript = redis.NewScript(`
local active_key = KEYS[1]
local token = ARGV[1]
local lease_ms = tonumber(ARGV[2])
if not redis.call('ZSCORE', active_key, token) then
  return 0
end
local redis_time = redis.call('TIME')
local now_ms = tonumber(redis_time[1]) * 1000 + math.floor(tonumber(redis_time[2]) / 1000)
redis.call('ZADD', active_key, 'XX', now_ms + lease_ms, token)
redis.call('PEXPIRE', active_key, lease_ms * 2)
return 1
`)

type ChannelTrafficAdmissionInfo struct {
	ChannelID         int    `json:"channel_id"`
	Queued            bool   `json:"queued"`
	QueuePosition     int    `json:"queue_position,omitempty"`
	WaitMilliseconds  int64  `json:"wait_ms"`
	Backend           string `json:"backend"`
	MaxConcurrency    int    `json:"max_concurrency,omitempty"`
	RPM               int    `json:"rpm,omitempty"`
	QueueCapacity     int    `json:"queue_capacity,omitempty"`
	QueueTimeout      int    `json:"queue_timeout_seconds,omitempty"`
	ActiveAtAdmission int    `json:"active_at_admission,omitempty"`
	RPMAtAdmission    int    `json:"rpm_at_admission,omitempty"`
}

type ChannelTrafficLease struct {
	Admission ChannelTrafficAdmissionInfo

	releaseOnce sync.Once
	done        chan struct{}
	release     func()
	renew       func() error
}

func (l *ChannelTrafficLease) Release() {
	if l == nil {
		return
	}
	l.releaseOnce.Do(func() {
		close(l.done)
		if l.release != nil {
			l.release()
		}
	})
}

func newChannelTrafficLease(ctx context.Context, admission ChannelTrafficAdmissionInfo, release func(), renew func() error) *ChannelTrafficLease {
	lease := &ChannelTrafficLease{
		Admission: admission,
		done:      make(chan struct{}),
		release:   release,
		renew:     renew,
	}
	if release == nil {
		return lease
	}
	go lease.monitor(ctx)
	return lease
}

func (l *ChannelTrafficLease) monitor(ctx context.Context) {
	var ticker *time.Ticker
	var heartbeat <-chan time.Time
	if l.renew != nil {
		ticker = time.NewTicker(channelTrafficHeartbeat)
		heartbeat = ticker.C
		defer ticker.Stop()
	}
	for {
		select {
		case <-ctx.Done():
			l.Release()
			return
		case <-l.done:
			return
		case <-heartbeat:
			if err := l.renew(); err != nil {
				logChannelTrafficRedisFallback(err)
			}
		}
	}
}

type channelTrafficResponseBody struct {
	io.ReadCloser
	lease *ChannelTrafficLease
}

func (b *channelTrafficResponseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.lease.Release()
	}
	return n, err
}

func (b *channelTrafficResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.lease.Release()
	return err
}

func HoldChannelTrafficUntilResponseClosed(body io.ReadCloser, lease *ChannelTrafficLease) io.ReadCloser {
	if lease == nil {
		return body
	}
	if body == nil {
		lease.Release()
		return nil
	}
	return &channelTrafficResponseBody{ReadCloser: body, lease: lease}
}

// DoChannelTrafficHTTPRequest applies channel admission control around provider
// HTTP calls that do not use relay/channel.DoRequest, such as background task
// polling. The concurrency lease remains held until the response body is read
// to completion or closed.
func DoChannelTrafficHTTPRequest(
	ctx context.Context,
	channelID int,
	config *dto.ChannelTrafficControl,
	request func() (*http.Response, error),
) (*http.Response, error) {
	lease, err := AcquireChannelTraffic(ctx, channelID, config)
	if err != nil {
		return nil, err
	}
	response, err := request()
	if err != nil {
		lease.Release()
		return nil, err
	}
	if response == nil {
		lease.Release()
		return nil, errors.New("channel traffic request returned nil response")
	}
	response.Body = HoldChannelTrafficUntilResponseClosed(response.Body, lease)
	return response, nil
}

func AcquireChannelTraffic(ctx context.Context, channelID int, config *dto.ChannelTrafficControl) (*ChannelTrafficLease, error) {
	if config == nil || !config.Effective() {
		return nil, nil
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if common.RedisEnabled && common.RDB != nil {
		lease, err := acquireRedisChannelTraffic(ctx, common.RDB, channelID, *config)
		if err == nil || isChannelTrafficAdmissionError(err) || ctx.Err() != nil {
			return lease, err
		}
		logChannelTrafficRedisFallback(err)
		return channelTrafficMemory.acquire(ctx, channelID, *config, "memory_fallback")
	}
	return channelTrafficMemory.acquire(ctx, channelID, *config, "memory")
}

func RecordChannelTrafficAdmission(c *gin.Context, lease *ChannelTrafficLease) {
	if c == nil || lease == nil {
		return
	}
	admissions := make([]ChannelTrafficAdmissionInfo, 0, 1)
	if existing, ok := c.Get(channelTrafficContextKey); ok {
		if values, valuesOK := existing.([]ChannelTrafficAdmissionInfo); valuesOK {
			admissions = append(admissions, values...)
		}
	}
	admissions = append(admissions, lease.Admission)
	c.Set(channelTrafficContextKey, admissions)
}

func AppendChannelTrafficAdminInfo(c *gin.Context, adminInfo map[string]interface{}) {
	if c == nil || adminInfo == nil {
		return
	}
	value, ok := c.Get(channelTrafficContextKey)
	if !ok {
		return
	}
	admissions, ok := value.([]ChannelTrafficAdmissionInfo)
	if !ok || len(admissions) == 0 {
		return
	}
	if len(admissions) == 1 {
		adminInfo["channel_traffic"] = admissions[0]
		return
	}
	adminInfo["channel_traffic"] = admissions
}

func isChannelTrafficAdmissionError(err error) bool {
	var apiErr *types.NewAPIError
	return errors.As(err, &apiErr) && types.IsChannelTrafficControlError(apiErr)
}

func newChannelTrafficAdmissionError(channelID int, code types.ErrorCode) *types.NewAPIError {
	message := fmt.Sprintf("channel #%d traffic queue is full", channelID)
	if code == types.ErrorCodeChannelTrafficQueueTimeout {
		message = fmt.Sprintf("channel #%d traffic queue wait timed out", channelID)
	}
	return types.NewErrorWithStatusCode(
		errors.New(message),
		code,
		http.StatusServiceUnavailable,
		types.ErrOptionWithNoRecordErrorLog(),
	)
}

func acquireRedisChannelTraffic(ctx context.Context, client *redis.Client, channelID int, config dto.ChannelTrafficControl) (*ChannelTrafficLease, error) {
	start := time.Now()
	token := uuid.NewString()
	keys := channelTrafficRedisKeys(channelID)
	deadline := start
	if config.QueueSize > 0 {
		deadline = deadline.Add(time.Duration(config.QueueTimeoutSeconds) * time.Second)
	}
	queued := false
	initialPosition := 0

	for {
		if err := ctx.Err(); err != nil {
			cancelRedisChannelTrafficQueue(client, keys[0], token)
			return nil, err
		}
		if config.QueueSize > 0 && !time.Now().Before(deadline) {
			cancelRedisChannelTrafficQueue(client, keys[0], token)
			return nil, newChannelTrafficAdmissionError(channelID, types.ErrorCodeChannelTrafficQueueTimeout)
		}

		result, err := channelTrafficAdmissionScript.Run(
			ctx,
			client,
			keys,
			token,
			config.MaxConcurrency,
			config.RPM,
			config.QueueSize,
			config.QueueTimeoutSeconds*1000,
			channelTrafficLeaseDuration.Milliseconds(),
		).Result()
		if err != nil {
			cancelRedisChannelTrafficQueue(client, keys[0], token)
			return nil, err
		}
		admission, err := parseRedisChannelTrafficAdmission(result)
		if err != nil {
			cancelRedisChannelTrafficQueue(client, keys[0], token)
			return nil, err
		}

		switch admission.status {
		case channelTrafficAcquired:
			info := ChannelTrafficAdmissionInfo{
				ChannelID:         channelID,
				Queued:            queued,
				QueuePosition:     initialPosition,
				WaitMilliseconds:  time.Since(start).Milliseconds(),
				Backend:           "redis",
				MaxConcurrency:    config.MaxConcurrency,
				RPM:               config.RPM,
				QueueCapacity:     config.QueueSize,
				QueueTimeout:      config.QueueTimeoutSeconds,
				ActiveAtAdmission: admission.active,
				RPMAtAdmission:    admission.rpm,
			}
			var release func()
			var renew func() error
			if config.MaxConcurrency > 0 {
				release = func() { releaseRedisChannelTraffic(client, keys[1], token) }
				renew = func() error { return renewRedisChannelTraffic(client, keys[1], token) }
			}
			return newChannelTrafficLease(ctx, info, release, renew), nil
		case channelTrafficFull:
			cancelRedisChannelTrafficQueue(client, keys[0], token)
			return nil, newChannelTrafficAdmissionError(channelID, types.ErrorCodeChannelTrafficQueueFull)
		case channelTrafficQueued:
			queued = true
			if initialPosition == 0 {
				initialPosition = admission.position
			}
		default:
			cancelRedisChannelTrafficQueue(client, keys[0], token)
			return nil, fmt.Errorf("invalid channel traffic admission status: %d", admission.status)
		}

		wait := admission.retryAfter
		if wait <= 0 {
			wait = channelTrafficConcurrencyRetry
		}
		if config.QueueSize > 0 {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				continue
			}
			if wait > remaining {
				wait = remaining
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			stopChannelTrafficTimer(timer)
			cancelRedisChannelTrafficQueue(client, keys[0], token)
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

type redisChannelTrafficAdmission struct {
	status     int
	retryAfter time.Duration
	active     int
	position   int
	rpm        int
}

func parseRedisChannelTrafficAdmission(value interface{}) (redisChannelTrafficAdmission, error) {
	items, ok := value.([]interface{})
	if !ok || len(items) < 5 {
		return redisChannelTrafficAdmission{}, fmt.Errorf("invalid channel traffic Redis response: %T", value)
	}
	values := make([]int64, 5)
	for i := range values {
		parsed, err := redisChannelTrafficInt(items[i])
		if err != nil {
			return redisChannelTrafficAdmission{}, err
		}
		values[i] = parsed
	}
	return redisChannelTrafficAdmission{
		status:     int(values[0]),
		retryAfter: time.Duration(values[1]) * time.Millisecond,
		active:     int(values[2]),
		position:   int(values[3]),
		rpm:        int(values[4]),
	}, nil
}

func redisChannelTrafficInt(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("invalid channel traffic Redis integer: %T", value)
	}
}

func channelTrafficRedisKeys(channelID int) []string {
	// The hash tag keeps all keys in one Redis Cluster slot so the admission
	// Lua script remains atomic on both standalone Redis and Redis Cluster.
	prefix := fmt.Sprintf("%s{%d}:", channelTrafficRedisPrefix, channelID)
	return []string{prefix + "queue", prefix + "active", prefix + "rpm", prefix + "sequence"}
}

func cancelRedisChannelTrafficQueue(client *redis.Client, queueKey string, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = client.ZRem(ctx, queueKey, token).Err()
}

func releaseRedisChannelTraffic(client *redis.Client, activeKey string, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.ZRem(ctx, activeKey, token).Err(); err != nil {
		logChannelTrafficRedisFallback(err)
	}
}

func renewRedisChannelTraffic(client *redis.Client, activeKey string, token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	renewed, err := channelTrafficRenewScript.Run(
		ctx,
		client,
		[]string{activeKey},
		token,
		channelTrafficLeaseDuration.Milliseconds(),
	).Int()
	if err != nil {
		return err
	}
	if renewed == 0 {
		return errors.New("channel traffic Redis lease expired before renewal")
	}
	return nil
}

var channelTrafficRedisWarning struct {
	sync.Mutex
	last time.Time
}

func logChannelTrafficRedisFallback(err error) {
	if err == nil {
		return
	}
	channelTrafficRedisWarning.Lock()
	defer channelTrafficRedisWarning.Unlock()
	if time.Since(channelTrafficRedisWarning.last) < time.Minute {
		return
	}
	channelTrafficRedisWarning.last = time.Now()
	common.SysLog("channel traffic control Redis unavailable, using local coordination: " + err.Error())
}

type channelTrafficMemoryManager struct {
	mu     sync.Mutex
	states map[int]*channelTrafficMemoryState
}

type channelTrafficMemoryState struct {
	mu      sync.Mutex
	active  map[string]struct{}
	queue   []string
	buckets [60]channelTrafficRPMBucket
	notify  chan struct{}
}

type channelTrafficRPMBucket struct {
	second int64
	count  int
}

type localChannelTrafficAdmission struct {
	status     int
	position   int
	retryAfter time.Duration
	active     int
	rpm        int
	notify     <-chan struct{}
}

var channelTrafficMemory = &channelTrafficMemoryManager{states: make(map[int]*channelTrafficMemoryState)}

func (m *channelTrafficMemoryManager) state(channelID int) *channelTrafficMemoryState {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.states[channelID]
	if state == nil {
		state = &channelTrafficMemoryState{
			active: make(map[string]struct{}),
			notify: make(chan struct{}),
		}
		m.states[channelID] = state
	}
	return state
}

func (m *channelTrafficMemoryManager) acquire(ctx context.Context, channelID int, config dto.ChannelTrafficControl, backend string) (*ChannelTrafficLease, error) {
	start := time.Now()
	token := uuid.NewString()
	state := m.state(channelID)
	deadline := start
	if config.QueueSize > 0 {
		deadline = deadline.Add(time.Duration(config.QueueTimeoutSeconds) * time.Second)
	}
	queued := false
	initialPosition := 0

	for {
		if err := ctx.Err(); err != nil {
			state.cancel(token)
			return nil, err
		}
		now := time.Now()
		if config.QueueSize > 0 && !now.Before(deadline) {
			state.cancel(token)
			return nil, newChannelTrafficAdmissionError(channelID, types.ErrorCodeChannelTrafficQueueTimeout)
		}
		admission := state.tryAcquire(token, config, now)
		switch admission.status {
		case channelTrafficAcquired:
			info := ChannelTrafficAdmissionInfo{
				ChannelID:         channelID,
				Queued:            queued,
				QueuePosition:     initialPosition,
				WaitMilliseconds:  time.Since(start).Milliseconds(),
				Backend:           backend,
				MaxConcurrency:    config.MaxConcurrency,
				RPM:               config.RPM,
				QueueCapacity:     config.QueueSize,
				QueueTimeout:      config.QueueTimeoutSeconds,
				ActiveAtAdmission: admission.active,
				RPMAtAdmission:    admission.rpm,
			}
			var release func()
			if config.MaxConcurrency > 0 {
				release = func() { state.release(token) }
			}
			return newChannelTrafficLease(ctx, info, release, nil), nil
		case channelTrafficFull:
			return nil, newChannelTrafficAdmissionError(channelID, types.ErrorCodeChannelTrafficQueueFull)
		case channelTrafficQueued:
			queued = true
			if initialPosition == 0 {
				initialPosition = admission.position
			}
		}

		wait := admission.retryAfter
		if wait <= 0 {
			wait = channelTrafficConcurrencyRetry
		}
		if config.QueueSize > 0 {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				continue
			}
			if wait > remaining {
				wait = remaining
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			stopChannelTrafficTimer(timer)
			state.cancel(token)
			return nil, ctx.Err()
		case <-admission.notify:
			stopChannelTrafficTimer(timer)
		case <-timer.C:
		}
	}
}

func (s *channelTrafficMemoryState) tryAcquire(token string, config dto.ChannelTrafficControl, now time.Time) localChannelTrafficAdmission {
	s.mu.Lock()
	defer s.mu.Unlock()

	position := s.queuePositionLocked(token)
	rpmCount, oldestSecond := s.rpmCountLocked(now)
	concurrencyAvailable := config.MaxConcurrency <= 0 || len(s.active) < config.MaxConcurrency
	rpmAvailable := config.RPM <= 0 || rpmCount < config.RPM

	if position < 0 {
		if len(s.queue) == 0 && concurrencyAvailable && rpmAvailable {
			return s.admitLocked(token, config, now, rpmCount)
		}
		if config.QueueSize <= 0 || len(s.queue) >= config.QueueSize {
			return localChannelTrafficAdmission{
				status: channelTrafficFull,
				active: len(s.active),
				rpm:    rpmCount,
				notify: s.notify,
			}
		}
		s.queue = append(s.queue, token)
		position = len(s.queue) - 1
	}

	if position == 0 && concurrencyAvailable && rpmAvailable {
		return s.admitLocked(token, config, now, rpmCount)
	}

	retryAfter := channelTrafficConcurrencyRetry
	if !rpmAvailable && oldestSecond > 0 {
		rpmRetry := time.Unix(oldestSecond+61, 0).Sub(now)
		if rpmRetry > retryAfter {
			retryAfter = rpmRetry
		}
	}
	return localChannelTrafficAdmission{
		status:     channelTrafficQueued,
		position:   position + 1,
		retryAfter: retryAfter,
		active:     len(s.active),
		rpm:        rpmCount,
		notify:     s.notify,
	}
}

func (s *channelTrafficMemoryState) admitLocked(token string, config dto.ChannelTrafficControl, now time.Time, rpmCount int) localChannelTrafficAdmission {
	if len(s.queue) > 0 && s.queue[0] == token {
		s.queue = s.queue[1:]
	}
	if config.MaxConcurrency > 0 {
		s.active[token] = struct{}{}
	}
	if config.RPM > 0 {
		s.recordRPMLocked(now)
		rpmCount++
	}
	s.signalLocked()
	return localChannelTrafficAdmission{
		status: channelTrafficAcquired,
		active: len(s.active),
		rpm:    rpmCount,
		notify: s.notify,
	}
}

func (s *channelTrafficMemoryState) release(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.active[token]; !exists {
		return
	}
	delete(s.active, token)
	s.signalLocked()
}

func (s *channelTrafficMemoryState) cancel(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	position := s.queuePositionLocked(token)
	if position < 0 {
		return
	}
	s.queue = append(s.queue[:position], s.queue[position+1:]...)
	s.signalLocked()
}

func (s *channelTrafficMemoryState) queuePositionLocked(token string) int {
	for i, queuedToken := range s.queue {
		if queuedToken == token {
			return i
		}
	}
	return -1
}

func (s *channelTrafficMemoryState) signalLocked() {
	close(s.notify)
	s.notify = make(chan struct{})
}

func (s *channelTrafficMemoryState) rpmCountLocked(now time.Time) (int, int64) {
	// One-second buckets intentionally expire conservatively at the next whole
	// second so the process-local fallback never exceeds the configured RPM.
	cutoff := now.Unix() - 60
	total := 0
	oldest := int64(0)
	for _, bucket := range s.buckets {
		if bucket.count == 0 || bucket.second < cutoff {
			continue
		}
		total += bucket.count
		if oldest == 0 || bucket.second < oldest {
			oldest = bucket.second
		}
	}
	return total, oldest
}

func (s *channelTrafficMemoryState) recordRPMLocked(now time.Time) {
	second := now.Unix()
	index := int(second % int64(len(s.buckets)))
	if index < 0 {
		index += len(s.buckets)
	}
	if s.buckets[index].second != second {
		s.buckets[index] = channelTrafficRPMBucket{second: second}
	}
	s.buckets[index].count++
}

func stopChannelTrafficTimer(timer *time.Timer) {
	if timer == nil || !timer.Stop() {
		return
	}
}
