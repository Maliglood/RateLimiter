// store/redis_store.go

package store

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var slidingWindowScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local member = ARGV[4]

-- Удаляем записи старше окна
redis.call('ZREMRANGEBYSCORE', key, '-inf', now - window_ms)

local count = redis.call('ZCARD', key)
if count < limit then
    redis.call('ZADD', key, now, member)
    redis.call('PEXPIRE', key, window_ms)
    return 1
end
return 0
`)

// Сервис хранилища в Redis
type RedisStore struct {
	client *redis.Client
	prefix string
	window time.Duration
}

// Создание нового экземпляра RedisStore
func NewRedisStore(client *redis.Client, prefix string, window time.Duration) *RedisStore {
	return &RedisStore{client: client, prefix: prefix, window: window}
}

// Allow проверяет, можно ли выполнить запрос для ключа key при заданном лимите. Возвращает:
//
//	allowed   — true, если запрос разрешён
//	retryAfter — через сколько времени можно повторить (для Retry-After)
//	error - описание ошибки
func (s *RedisStore) Allow(ctx context.Context, key string, limit int) (bool, time.Duration, error) {
	redisKey := fmt.Sprintf("%s:sw:%s", s.prefix, key)
	now := time.Now().UnixMilli()
	member := fmt.Sprintf("%d-%d", now, time.Now().UnixNano())

	result, err := slidingWindowScript.Run(ctx, s.client, []string{redisKey},
		now, s.window.Milliseconds(), limit, member,
	).Int64()

	if err != nil {
		return false, 0, fmt.Errorf("redis sliding window: %w", err)
	}

	if result == 1 {
		return true, 0, nil
	}

	// Вычисляем, через сколько освободится самый старый элемент окна
	oldest, err := s.client.ZRangeWithScores(ctx, redisKey, 0, 0).Result()

	if err != nil || len(oldest) == 0 {
		return false, s.window, nil // консервативный fallback
	}

	oldestMs := int64(oldest[0].Score)
	retryAfter := time.Duration(now-oldestMs) * time.Millisecond

	if retryAfter < 0 {
		retryAfter = 0
	}

	return false, retryAfter, nil
}

// Проверка работоспособности Redis'а
func (s *RedisStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

var _ Store = (*RedisStore)(nil) // Проверка реализации интерфейса Store
