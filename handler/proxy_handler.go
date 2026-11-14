// handler/proxy_handler.go
// Обработчик запроса:
// * извлекает лимит из query-параметра (или берёт дефолт из конфига),
// * вызывает лимитер
// * и либо проксирует запрос, либо возвращает 429 с Retry-After.

package handler

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"time"

	"rate-limiter/limiter"
)

// HTTP-обработчик
type ProxyHandler struct {
	limiter      *limiter.ResilientLimiter
	defaultLimit int
	logger       *slog.Logger
}

// Создание нового экземпляра обработчика
func NewProxyHandler(l *limiter.ResilientLimiter, defaultLimit int, logger *slog.Logger) *ProxyHandler {
	return &ProxyHandler{limiter: l, defaultLimit: defaultLimit, logger: logger}
}

// Обработка входящего HTTP-запроса:
// проверяет лимит запросов для пользователя и, если лимит не превышен, проксирует запрос на целевой URL.
// При превышении лимита возвращает "429 Too Many Requests" с заголовком Retry-After.
func (h *ProxyHandler) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	targetURL := request.URL.Query().Get("url")

	if targetURL == "" {
		http.Error(responseWriter, "missing 'url' query parameter", http.StatusBadRequest)
		return
	}

	limit := h.defaultLimit

	if raw := request.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			http.Error(responseWriter, "invalid 'limit' parameter", http.StatusBadRequest)
			return
		}
		limit = n
	}

	userID := request.Header.Get("X-User-ID") // Предпочтительный вариант

	if userID == "" {
		userID = request.URL.Query().Get("user_id") // Запасной вариант
	}

	if userID == "" {
		userID = request.RemoteAddr // В крайнем случае по IP
	}

	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()

	allowed, retryAfter := h.limiter.Allow(ctx, userID, limit)

	if !allowed {
		secs := int(retryAfter.Seconds())
		if secs < 1 {
			secs = 1
		}
		responseWriter.Header().Set("Retry-After", strconv.Itoa(secs))
		http.Error(responseWriter, "rate limit exceeded", http.StatusTooManyRequests)

		return
	}

	goalUrl, err := url.Parse(targetURL)

	if err != nil {
		http.Error(responseWriter, "invalid target url", http.StatusBadRequest)

		return
	}

	// Удаляем limit и url из query, чтобы не передавать их дальше
	newQueryParams := goalUrl.Query()
	newQueryParams.Del("limit")
	newQueryParams.Del("url")
	goalUrl.RawQuery = newQueryParams.Encode()

	h.logger.Info("Goal Url", "goalUrl", goalUrl)
	// proxy := httputil.NewSingleHostReverseProxy(goalUrl)
	// proxy.ErrorHandler = func(responseWriter http.ResponseWriter, request *http.Request, err error) {
	// 	h.logger.Error("proxy error", "target", goalUrl.String(), "err", err)
	// 	http.Error(responseWriter, "bad gateway", http.StatusBadGateway)
	// }
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL = goalUrl                                                    // Заменяем URL на целевой
			req.Host = goalUrl.Host                                              // Заменяем Host на новый
			h.logger.Info("proxying", "url", req.URL.String(), "host", req.Host) // Логгирование результата
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			h.logger.Error("proxy error", "target", goalUrl.String(), "err", err)
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}
	proxy.Transport = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		DisableKeepAlives:     true, // Для избегания переиспользования «мёртвых» соединений
	}

	// r.URL = parsed // Подменяем URL в исходном запросе, сохраняя остальные параметры
	proxy.ServeHTTP(responseWriter, request)
}
