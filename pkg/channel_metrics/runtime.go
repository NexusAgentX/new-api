package channelmetrics

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/go-redis/redis/v8"
)

const (
	runtimeLeaseTTL              = 90 * time.Second
	runtimeWindow                = time.Minute
	runtimeBackendTimeout        = time.Second
	runtimeFallbackProbeInterval = 5 * time.Second
	runtimeFallbackLogRate       = time.Minute
)

type RuntimeMode string

const (
	RuntimeModeRedis          RuntimeMode = "redis"
	RuntimeModeMemory         RuntimeMode = "memory"
	RuntimeModeMemoryFallback RuntimeMode = "memory_fallback"
)

type RuntimeSnapshot struct {
	Mode               RuntimeMode `json:"mode"`
	CurrentConcurrency int64       `json:"current_concurrency"`
	RPM                int64       `json:"rpm"`
}

type memoryRuntimeStart struct {
	startedAt time.Time
	mode      RuntimeMode
}

type memoryRuntimeState struct {
	mu        sync.Mutex
	active    map[string]RuntimeMode
	rpmStarts []memoryRuntimeStart
}

type runtimeManager struct {
	now            func() time.Time
	redisEnabled   func() bool
	redisClient    func() *redis.Client
	memoryStates   sync.Map
	fallbackUntil  atomic.Int64
	lastFallbackAt atomic.Int64
}

type runtimeLease struct {
	manager   *runtimeManager
	channelId int
	leaseId   string
	mode      RuntimeMode
	client    *redis.Client
	done      chan struct{}
	finish    sync.Once
}

var defaultRuntimeManager = &runtimeManager{
	now:          time.Now,
	redisEnabled: func() bool { return common.RedisEnabled && common.RDB != nil },
	redisClient:  func() *redis.Client { return common.RDB },
}

var runtimeStartScript = redis.NewScript(`
local now = tonumber(ARGV[1])
local active_expiry = tonumber(ARGV[2])
local rpm_cutoff = tonumber(ARGV[3])
local lease_id = ARGV[4]
local ttl = tonumber(ARGV[5])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', rpm_cutoff)
redis.call('ZADD', KEYS[1], active_expiry, lease_id)
redis.call('ZADD', KEYS[2], now, lease_id)
redis.call('PEXPIRE', KEYS[1], ttl)
redis.call('PEXPIRE', KEYS[2], ttl)
return {redis.call('ZCARD', KEYS[1]), redis.call('ZCARD', KEYS[2])}
`)

var runtimeSnapshotScript = redis.NewScript(`
local now = tonumber(ARGV[1])
local rpm_cutoff = tonumber(ARGV[2])
return {
  redis.call('ZCOUNT', KEYS[1], '(' .. now, '+inf'),
  redis.call('ZCOUNT', KEYS[2], '(' .. rpm_cutoff, '+inf')
}
`)

var runtimeRenewScript = redis.NewScript(`
if redis.call('ZSCORE', KEYS[1], ARGV[1]) then
  redis.call('ZADD', KEYS[1], tonumber(ARGV[2]), ARGV[1])
  redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[3]))
  return 1
end
return 0
`)

func runtimeActiveKey(channelId int) string {
	return fmt.Sprintf("channel:metrics:active:%d", channelId)
}

func runtimeRPMKey(channelId int) string {
	return fmt.Sprintf("channel:metrics:rpm:%d", channelId)
}

func (m *runtimeManager) begin(ctx context.Context, channelId int) (*runtimeLease, RuntimeSnapshot) {
	if ctx == nil {
		ctx = context.Background()
	}
	if m.redisEnabled != nil && m.redisEnabled() {
		if m.inFallbackWindow() {
			return m.beginMemory(channelId, RuntimeModeMemoryFallback)
		}
		lease, snapshot, err := m.beginRedis(ctx, channelId)
		if err == nil {
			m.fallbackUntil.Store(0)
			return lease, snapshot
		}
		m.enterFallbackWindow()
		m.logFallback(ctx, err)
		return m.beginMemory(channelId, RuntimeModeMemoryFallback)
	}
	return m.beginMemory(channelId, RuntimeModeMemory)
}

func (m *runtimeManager) beginRedis(parent context.Context, channelId int) (*runtimeLease, RuntimeSnapshot, error) {
	client := m.redisClient()
	if client == nil {
		return nil, RuntimeSnapshot{}, errors.New("Redis client is not initialized")
	}
	now := m.now()
	leaseId := common.GetUUID()
	ctx, cancel := context.WithTimeout(parent, runtimeBackendTimeout)
	defer cancel()
	values, err := runtimeStartScript.Run(ctx, client,
		[]string{runtimeActiveKey(channelId), runtimeRPMKey(channelId)},
		now.UnixMilli(),
		now.Add(runtimeLeaseTTL).UnixMilli(),
		now.Add(-runtimeWindow).UnixMilli(),
		leaseId,
		(runtimeLeaseTTL + runtimeWindow).Milliseconds(),
	).Int64Slice()
	if err != nil {
		return nil, RuntimeSnapshot{}, err
	}
	if len(values) != 2 {
		return nil, RuntimeSnapshot{}, fmt.Errorf("unexpected runtime metrics reply length %d", len(values))
	}
	lease := &runtimeLease{
		manager:   m,
		channelId: channelId,
		leaseId:   leaseId,
		mode:      RuntimeModeRedis,
		client:    client,
		done:      make(chan struct{}),
	}
	m.registerMemoryStart(channelId, leaseId, now, RuntimeModeRedis)
	fallbackConcurrency, fallbackRPM := m.nonRedisMemoryCounts(channelId, now)
	go lease.renewLoop()
	return lease, RuntimeSnapshot{
		Mode:               RuntimeModeRedis,
		CurrentConcurrency: values[0] + fallbackConcurrency,
		RPM:                values[1] + fallbackRPM,
	}, nil
}

func (m *runtimeManager) beginMemory(channelId int, mode RuntimeMode) (*runtimeLease, RuntimeSnapshot) {
	now := m.now()
	state := m.memoryState(channelId)
	state.mu.Lock()
	state.pruneRPM(now.Add(-runtimeWindow))
	leaseId := common.GetUUID()
	state.active[leaseId] = mode
	state.rpmStarts = append(state.rpmStarts, memoryRuntimeStart{startedAt: now, mode: mode})
	snapshot := RuntimeSnapshot{Mode: mode, CurrentConcurrency: int64(len(state.active)), RPM: int64(len(state.rpmStarts))}
	state.mu.Unlock()
	return &runtimeLease{manager: m, channelId: channelId, leaseId: leaseId, mode: mode, done: make(chan struct{})}, snapshot
}

func (m *runtimeManager) snapshot(parent context.Context, channelId int) RuntimeSnapshot {
	if parent == nil {
		parent = context.Background()
	}
	if m.redisEnabled != nil && m.redisEnabled() {
		if m.inFallbackWindow() {
			return m.memorySnapshot(channelId, RuntimeModeMemoryFallback)
		}
		client := m.redisClient()
		if client == nil {
			m.enterFallbackWindow()
			m.logFallback(parent, errors.New("Redis client is not initialized"))
			return m.memorySnapshot(channelId, RuntimeModeMemoryFallback)
		}
		now := m.now()
		ctx, cancel := context.WithTimeout(parent, runtimeBackendTimeout)
		values, err := runtimeSnapshotScript.Run(ctx, client,
			[]string{runtimeActiveKey(channelId), runtimeRPMKey(channelId)},
			now.UnixMilli(), now.Add(-runtimeWindow).UnixMilli(),
		).Int64Slice()
		cancel()
		if err == nil && len(values) == 2 {
			m.fallbackUntil.Store(0)
			fallbackConcurrency, fallbackRPM := m.nonRedisMemoryCounts(channelId, now)
			return RuntimeSnapshot{
				Mode:               RuntimeModeRedis,
				CurrentConcurrency: values[0] + fallbackConcurrency,
				RPM:                values[1] + fallbackRPM,
			}
		}
		if err == nil {
			err = fmt.Errorf("unexpected runtime metrics reply length %d", len(values))
		}
		m.enterFallbackWindow()
		m.logFallback(parent, err)
		return m.memorySnapshot(channelId, RuntimeModeMemoryFallback)
	}
	return m.memorySnapshot(channelId, RuntimeModeMemory)
}

func (m *runtimeManager) memorySnapshot(channelId int, mode RuntimeMode) RuntimeSnapshot {
	state, exists := m.loadMemoryState(channelId)
	if !exists {
		return RuntimeSnapshot{Mode: mode}
	}
	state.mu.Lock()
	state.pruneRPM(m.now().Add(-runtimeWindow))
	snapshot := RuntimeSnapshot{Mode: mode, CurrentConcurrency: int64(len(state.active)), RPM: int64(len(state.rpmStarts))}
	state.mu.Unlock()
	return snapshot
}

func (m *runtimeManager) memoryState(channelId int) *memoryRuntimeState {
	state := &memoryRuntimeState{active: make(map[string]RuntimeMode)}
	actual, _ := m.memoryStates.LoadOrStore(channelId, state)
	return actual.(*memoryRuntimeState)
}

func (m *runtimeManager) loadMemoryState(channelId int) (*memoryRuntimeState, bool) {
	state, exists := m.memoryStates.Load(channelId)
	if !exists {
		return nil, false
	}
	return state.(*memoryRuntimeState), true
}

func (m *runtimeManager) registerMemoryStart(channelId int, leaseId string, startedAt time.Time, mode RuntimeMode) {
	state := m.memoryState(channelId)
	state.mu.Lock()
	state.pruneRPM(startedAt.Add(-runtimeWindow))
	state.active[leaseId] = mode
	state.rpmStarts = append(state.rpmStarts, memoryRuntimeStart{startedAt: startedAt, mode: mode})
	state.mu.Unlock()
}

func (m *runtimeManager) nonRedisMemoryCounts(channelId int, now time.Time) (int64, int64) {
	state, exists := m.loadMemoryState(channelId)
	if !exists {
		return 0, 0
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.pruneRPM(now.Add(-runtimeWindow))
	var concurrency int64
	for _, mode := range state.active {
		if mode != RuntimeModeRedis {
			concurrency++
		}
	}
	var rpm int64
	for _, start := range state.rpmStarts {
		if start.mode != RuntimeModeRedis {
			rpm++
		}
	}
	return concurrency, rpm
}

func (s *memoryRuntimeState) pruneRPM(cutoff time.Time) {
	first := 0
	for first < len(s.rpmStarts) && !s.rpmStarts[first].startedAt.After(cutoff) {
		first++
	}
	if first > 0 {
		s.rpmStarts = append([]memoryRuntimeStart(nil), s.rpmStarts[first:]...)
	}
}

func (l *runtimeLease) close() {
	if l == nil {
		return
	}
	l.finish.Do(func() {
		close(l.done)
		switch l.mode {
		case RuntimeModeRedis:
			ctx, cancel := context.WithTimeout(context.Background(), runtimeBackendTimeout)
			if err := l.client.ZRem(ctx, runtimeActiveKey(l.channelId), l.leaseId).Err(); err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("release channel metrics runtime lease failed: %v", err))
			}
			cancel()
		}
		state := l.manager.memoryState(l.channelId)
		state.mu.Lock()
		delete(state.active, l.leaseId)
		state.mu.Unlock()
	})
}

func (l *runtimeLease) renewLoop() {
	interval := runtimeLeaseTTL / 3
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-l.done:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), runtimeBackendTimeout)
			result, err := runtimeRenewScript.Run(ctx, l.client, []string{runtimeActiveKey(l.channelId)}, l.leaseId, l.manager.now().Add(runtimeLeaseTTL).UnixMilli(), (runtimeLeaseTTL + runtimeWindow).Milliseconds()).Int()
			cancel()
			if err != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf("renew channel metrics runtime lease failed: %v", err))
				continue
			}
			if result == 0 {
				return
			}
		}
	}
}

func (m *runtimeManager) inFallbackWindow() bool {
	until := m.fallbackUntil.Load()
	return until > 0 && m.now().UnixNano() < until
}

func (m *runtimeManager) enterFallbackWindow() {
	m.fallbackUntil.Store(m.now().Add(runtimeFallbackProbeInterval).UnixNano())
}

func (m *runtimeManager) logFallback(ctx context.Context, err error) {
	now := m.now().Unix()
	last := m.lastFallbackAt.Load()
	if last != 0 && now-last < int64(runtimeFallbackLogRate/time.Second) {
		return
	}
	if m.lastFallbackAt.CompareAndSwap(last, now) {
		logger.LogWarn(ctx, fmt.Sprintf("channel metrics Redis unavailable; using per-process runtime values: %v", err))
	}
}
