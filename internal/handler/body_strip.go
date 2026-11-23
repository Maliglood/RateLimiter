// internal/handler/body_strip.go
package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Удаляет указанные поля из JSON-объекта в теле запроса.
//
// ПРЕДУПРЕЖДЕНИЕ: unmarshal в map[string]json.RawMessage переупорядочивает поля (по алфавиту) и удаляет форматирование.
// Если клиент подписывает тело (HMAC/JWS) — этот подход сломает подпись.
func stripJSONFields(request *http.Request, deletingFields ...string) error {
	if request.Body == nil || request.Body == http.NoBody {
		return nil
	}

	if !strings.HasPrefix(request.Header.Get("Content-Type"), "application/json") {
		return nil
	}

	body, err := io.ReadAll(request.Body)
	_ = request.Body.Close()

	if err != nil {
		return err
	}

	// Восстанавливаем тело (всегда)
	defer func() {
		if request.Body == nil || request.Body == http.NoBody {
			request.Body = io.NopCloser(bytes.NewReader(body))
		}
	}()

	var obj map[string]json.RawMessage

	if err := json.Unmarshal(body, &obj); err != nil {
		return nil // Если не объект - то оставляем как есть
	}

	changed := false

	for _, field := range deletingFields {
		if _, ok := obj[field]; ok {
			delete(obj, field)
			changed = true
		}
	}

	if !changed {
		return nil
	}

	newBody, err := json.Marshal(obj)

	if err != nil {
		return err
	}

	request.Body = io.NopCloser(bytes.NewReader(newBody))
	request.ContentLength = int64(len(newBody))
	request.Header.Set("Content-Length", strconv.Itoa(len(newBody)))

	return nil
}
