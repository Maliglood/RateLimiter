// store/store.go

package store

import (
	"context"
	"time"
)

// Store — общий контракт для реализации хранения счётчиков.
type Store interface {
	// Allow проверяет, можно ли выполнить запрос для ключа key при заданном лимите. Возвращает:
	//   allowed   — true, если запрос разрешён
	//   retryAfter — через сколько времени можно повторить (для Retry-After)
	//   error - описание ошибки
	Allow(ctx context.Context, key string, limit int) (bool, time.Duration, error)
}
