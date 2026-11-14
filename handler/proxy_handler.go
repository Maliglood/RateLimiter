// handler/proxy_handler.go
// Обработчик запроса:
// * извлекает лимит из query-параметра (или берёт дефолт из конфига),
// * вызывает лимитер
// * и либо проксирует запрос, либо возвращает 429 с Retry-After.

package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"time"

	"rate-limiter/limiter"
)

const (
	// maxLimit — верхняя граница ?limit, чтобы нельзя было запросить «безлимит».
	maxLimit = 1_000_000

	// limiterTimeout — предел времени на проверку лимита (Redis/память).
	limiterTimeout = 2 * time.Second
)

// Проксирует HTTP-запросы с предварительной проверкой rate limit.
type ProxyHandler struct {
	limiter      *limiter.ResilientLimiter
	defaultLimit int
	logger       *slog.Logger
	transport    http.RoundTripper
}

func newTransport() *http.Transport {
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		DisableKeepAlives:     true,
	}
}

// Создание нового экземпляра прокси-хэндлера
func NewProxyHandler(l *limiter.ResilientLimiter, defaultLimit int, logger *slog.Logger) *ProxyHandler {
	return &ProxyHandler{
		limiter:      l,
		defaultLimit: defaultLimit,
		logger:       logger,
		transport:    newTransport(),
	}
}

// Обработка входящего HTTP-запроса:
// проверяет лимит запросов для пользователя и, если лимит не превышен, проксирует запрос на целевой URL.
// При превышении лимита возвращает "429 Too Many Requests" с заголовком Retry-After.
func (handler *ProxyHandler) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	limit, err := handler.getLimit(request)

	if err != nil {
		http.Error(responseWriter, err.Error(), http.StatusBadRequest)

		return
	}

	userId := getUserId(request)
	allowed, retryAfter := handler.allow(request.Context(), userId, limit)

	if !allowed {
		handler.writeRateLimitedError(responseWriter, retryAfter)

		return
	}

	goalUrl, err := handler.getGoalUrl(request)

	if err != nil {
		http.Error(responseWriter, err.Error(), http.StatusBadRequest)
		return
	}

	handler.proxy(responseWriter, request, goalUrl)
}

// Вычисление лимита для текущего запроса.
func (handler *ProxyHandler) getLimit(request *http.Request) (int, error) {
	raw := request.URL.Query().Get("limit")

	if raw == "" {
		return handler.defaultLimit, nil
	}

	n, err := strconv.Atoi(raw)

	if err != nil {
		return 0, fmt.Errorf("Invalid 'limit' parameter: %v", err)
	}

	if n <= 0 || n > maxLimit {
		return 0, fmt.Errorf("'limit' must be between 1 and %d", maxLimit)
	}

	return n, nil
}

// Вычисление идентификатора пользователя
func getUserId(request *http.Request) string {
	userId := request.Header.Get("X-User-ID") // Предпочтительный вариант

	if userId == "" {
		userId = request.URL.Query().Get("user_id") // Запасной вариант
	}

	if userId == "" {
		// В крайнем случае по IP
		if host, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
			userId = host // Отбрасываем порт из "ip:port".
		} else {
			userId = request.RemoteAddr
		}
	}

	return userId
}

// Вычисляет целевой URL, обрабатывает (если нужно) и убирает лишние параметры
func (handler *ProxyHandler) getGoalUrl(r *http.Request) (*url.URL, error) {
	rawUrl := r.URL.Query().Get("url")

	if rawUrl == "" {
		return nil, errors.New("missing 'url' query parameter")
	}

	goalUrl, err := url.Parse(rawUrl)

	if err != nil {
		return nil, fmt.Errorf("invalid target url: %w", err)
	}

	if goalUrl.Scheme != "http" && goalUrl.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q: only http and https are allowed", goalUrl.Scheme)
	}

	if goalUrl.Host == "" {
		return nil, errors.New("target url must include a host")
	}

	// Удаляем limit и url из query, чтобы не передавать их дальше
	q := goalUrl.Query()
	q.Del("limit")
	q.Del("url")
	goalUrl.RawQuery = q.Encode()

	handler.logger.Info("Goal Url", "goalUrl", goalUrl)

	return goalUrl, nil
}

// Проверяет лимит с ограниченным таймаутом.
func (handler *ProxyHandler) allow(parent context.Context, userID string, limit int) (bool, time.Duration) {
	ctx, cancel := context.WithTimeout(parent, limiterTimeout)
	defer cancel()

	return handler.limiter.Allow(ctx, userID, limit)
}

// Возвращает 429 с заголовком Retry-After.
func (handler *ProxyHandler) writeRateLimitedError(writer http.ResponseWriter, retryAfter time.Duration) {
	secs := int(retryAfter.Seconds())

	if secs < 1 {
		secs = 1
	}

	writer.Header().Set("Retry-After", strconv.Itoa(secs))
	http.Error(writer, "rate limit exceeded", http.StatusTooManyRequests)
}

// Пересылает запрос на целевой URL.
func (handler *ProxyHandler) proxy(writer http.ResponseWriter, request *http.Request, targetUrl *url.URL) {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			// Полностью заменяем URL — joinURLPath не нужен.
			pr.Out.URL = targetUrl
			pr.Out.Host = targetUrl.Host
			handler.logger.Debug("proxying", "url", targetUrl.String(), "host", targetUrl.Host)
		},
		Transport: handler.transport,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			handler.logger.Error("proxy error", "target", targetUrl.String(), "err", err)
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(writer, request)
}
