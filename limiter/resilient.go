// internal/limiter/resilient.go

package limiter

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"rate-limiter/store"
)

type RedisStore = store.RedisStore
type MemoryStore = store.MemoryStore

// Обёртка, которая пытается выполнить операцию в Redis, а при ошибке (таймаут, connection refused) автоматически переключается на MemoryStore.
type ResilientLimiter struct {
	redis   *RedisStore
	memory  *MemoryStore
	redisUp atomic.Bool
	logger  *slog.Logger
	stopCh  chan struct{}
}

// Создание нового экземпляра ResilientLimiter
func NewResilientLimiter(redisStore *RedisStore, memStore *MemoryStore, logger *slog.Logger) *ResilientLimiter {
	rl := &ResilientLimiter{
		redis:  redisStore,
		memory: memStore,
		logger: logger,
		stopCh: make(chan struct{}),
	}

	rl.redisUp.Store(true)
	go rl.healthLoop()
	return rl
}

func (r *ResilientLimiter) Allow(ctx context.Context, key string, limit int) (bool, time.Duration) {
	if r.redisUp.Load() {
		allowed, retryAfter, err := r.redis.Allow(ctx, key, limit)

		if err == nil {
			return allowed, retryAfter
		}

		r.logger.Warn("redis unavailable, falling back to memory", "err", err)
		r.redisUp.Store(false)
	}

	// Fallback на память
	allowed, retryAfter, err := r.memory.Allow(ctx, key, limit)

	if err == nil {
		return allowed, retryAfter
	}

	r.logger.Warn("Error", "err", err)
	r.redisUp.Store(false)

	return false, 0
}

// Фоновый health-check восстанавливает работу с Redis, когда он снова становится доступен.
func (r *ResilientLimiter) healthLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if r.redisUp.Load() {
				continue // уже работает
			}

			pingCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			err := r.redis.Ping(pingCtx)

			cancel()

			if err == nil {
				r.logger.Info("redis recovered, switching back")
				r.redisUp.Store(true)
			}
		case <-r.stopCh:
			return
		}
	}
}

// Каскадная остановка всех фоновых горутин
func (r *ResilientLimiter) Close() {
	close(r.stopCh)
	r.memory.Close()
}
