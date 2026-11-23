// internal/handler/proxy_handler.go
// Обработчик запроса:
// * извлекает лимит из query-параметра (или берёт дефолт из конфига),
// * вызывает лимитер
// * и либо проксирует запрос, либо возвращает 429 с Retry-After.

package handler

import (
	"net/http"
)

func CORS(next http.Handler, allowOrigin string) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		responseWriter.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		responseWriter.Header().Set("Vary", "Origin")
		responseWriter.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		responseWriter.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-User-ID")
		responseWriter.Header().Set("Access-Control-Expose-Headers", "Retry-After")
		responseWriter.Header().Set("Access-Control-Max-Age", "86400")

		if request.Method == http.MethodOptions && request.Header.Get("Access-Control-Request-Method") != "" {
			responseWriter.WriteHeader(http.StatusNoContent)
			return // preflight не идёт в лимитер и в прокси
		}
		next.ServeHTTP(responseWriter, request)
	})
}
