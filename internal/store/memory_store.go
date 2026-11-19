// store/memory_store.go

package store

import (
	"context"
	"sync"
	"time"
)

// Структура единицы записи
type memoryEntry struct {
	mu         sync.Mutex
	timestamps []time.Time
}

// Сервис локального хранилища
type MemoryStore struct {
	data   sync.Map // map[string]*memoryEntry
	window time.Duration
	stopCh chan struct{}
}

// Создание нового экземпляра MemoryStore
func NewMemoryStore(window time.Duration) *MemoryStore {
	m := &MemoryStore{
		window: window,
		stopCh: make(chan struct{}),
	}
	go m.cleanupLoop()
	return m
}

// Allow проверяет, можно ли выполнить запрос для ключа key при заданном лимите. Возвращает:
//
//	allowed   — true, если запрос разрешён
//	retryAfter — через сколько времени можно повторить (для Retry-After)
//	error - описание ошибки
func (m *MemoryStore) Allow(_ context.Context, key string, limit int) (bool, time.Duration, error) {
	now := time.Now()
	cutoff := now.Add(-m.window)
	val, _ := m.data.LoadOrStore(key, &memoryEntry{})
	entry := val.(*memoryEntry)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	// Удаляем устаревшие метки
	idx := 0

	for idx < len(entry.timestamps) && entry.timestamps[idx].Before(cutoff) {
		idx++
	}

	entry.timestamps = entry.timestamps[idx:]

	if len(entry.timestamps) < limit {
		entry.timestamps = append(entry.timestamps, now)
		return true, 0, nil
	}

	// Retry-After = время до истечения самой старой метки
	retryAfter := entry.timestamps[0].Add(m.window).Sub(now)

	if retryAfter < 0 {
		retryAfter = 0
	}

	return false, retryAfter, nil
}

// Очистка локальной памяти
func (m *MemoryStore) cleanupLoop() {
	ticker := time.NewTicker(2 * m.window)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cutoff := time.Now().Add(-3 * m.window)
			m.data.Range(func(k, v any) bool {
				entry := v.(*memoryEntry)
				entry.mu.Lock()
				if len(entry.timestamps) == 0 || entry.timestamps[len(entry.timestamps)-1].Before(cutoff) {
					m.data.Delete(k)
				}
				entry.mu.Unlock()
				return true
			})
		case <-m.stopCh:
			return
		}
	}
}

// Остановка фоновой горутины
func (m *MemoryStore) Close() { close(m.stopCh) } // TODO: Сделать защиту от повторного закрытия через sync.Once

var _ Store = (*MemoryStore)(nil) // Проверка реализации интерфейса Store
