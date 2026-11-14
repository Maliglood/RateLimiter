# Rate Limiter Service на Go

Веб-сервис для ограничения количества запросов от одного пользователя в минуту с автоматическим переходом на резервный in-memory кэш при недоступности Redis.

## Возможности

- **Sliding Window** алгоритм подсчёта запросов (точнее фиксированного окна)
- **Redis** как основной бэкенд с атомарными Lua-скриптами
- **Автоматический fallback** на in-memory хранилище при сбое Redis
- **Восстановление** работы с Redis через фоновый health-check
- **Проксирование** запросов с очисткой служебных параметров (`url`, `limit`)
- **Гибкая настройка** лимита через query-параметр или конфигурацию

## Быстрый старт

### Требования
- Go 1.21+
- Redis 6+ (опционально, сервис работает и без него)

### Запуск
```bash
# 1. Клонировать репозиторий
git clone https://github.com/yourusername/ratelimiter.git
cd ratelimiter

# 2. Установить зависимости
go mod download

# 3. Запустить Redis (если есть Docker)
docker run -d -p 6379:6379 redis:alpine

# 4. Запустить сервис
go run cmd/server/main.go
```

Сервис будет доступен по адресу `http://localhost:8080`.

## Конфигурация

| Переменная | По умолчанию | Описание |
|---|---|---|
| `REDIS_ADDR` | `localhost:6379` | Адрес Redis |
| `DEFAULT_LIMIT` | `60` | Лимит запросов в минуту по умолчанию |
| `LISTEN_ADDR` | `:8080` | Адрес прослушивания |

## Использование

```bash
# Запрос с явным лимитом (10 запросов/мин)
curl "http://localhost:8080/?url=https://api.example.com/data&limit=10&user_id=alice"

# Запрос с лимитом по умолчанию
curl -H "X-User-ID: bob" \
  "http://localhost:8080/?url=https://api.example.com/data"
```

## Архитектура

```
Client → HTTP Handler → Resilient Limiter → RedisStore (primary)
                                          ↘ MemoryStore (fallback)
```

- **HTTP Handler** — извлекает параметры, проверяет лимит, проксирует запрос
- **Resilient Limiter** — переключается между Redis и памятью при сбоях
- **RedisStore** — атомарный sliding window на Lua-скрипте
- **MemoryStore** — потокобезопасный резервный счётчик с периодической очисткой

## Тестирование

```bash
go test ./...
```

## Лицензия

MIT