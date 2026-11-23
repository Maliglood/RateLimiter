// internal/handler/body_strip.go
package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
)

// Диспетчер: выбирает стратегию по Content-Type.
// Возвращает nil, если тело не JSON/form/multipart или ничего не изменилось.
func stripFromBody(r *http.Request, fields ...string) error {
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}

	mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))

	switch mediaType {
	case "application/json":
		return stripJSONFields(r, fields...)
	case "application/x-www-form-urlencoded":
		return stripFormFields(r, fields...)
	case "multipart/form-data":
		return stripMultipartFields(r, params["boundary"], fields...)
	}

	return nil
}

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

	request.Body = io.NopCloser(bytes.NewReader(body))
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

// Удаляет поля из url-encoded тела.
func stripFormFields(r *http.Request, fields ...string) error {
	body, err := io.ReadAll(r.Body)
	_ = r.Body.Close()

	if err != nil {
		return err
	}

	r.Body = io.NopCloser(bytes.NewReader(body)) // восстановили сразу
	values, err := url.ParseQuery(string(body))

	if err != nil {
		return nil // кривая форма — не трогаем
	}

	changed := false

	for _, f := range fields {
		if values.Has(f) {
			values.Del(f)
			changed = true
		}
	}

	if !changed {
		return nil
	}

	newBody := []byte(values.Encode())
	r.Body = io.NopCloser(bytes.NewReader(newBody))
	r.ContentLength = int64(len(newBody))
	r.Header.Set("Content-Length", strconv.Itoa(len(newBody)))

	return nil
}

// Пересобирает multipart-тело, пропуская указанные поля.
//
// ПРЕДУПРЕЖДЕНИЕ: multipart.Writer переупорядочивает заголовки частей (map iteration) и нормализует CRLF.
// Для файлов с подписью/хэшем в заголовках это может сломать проверку.
// Если апстрим чувствителен — не используйте.
func stripMultipartFields(r *http.Request, boundary string, fields ...string) error {
	if boundary == "" {
		return nil
	}

	body, err := io.ReadAll(r.Body)
	_ = r.Body.Close()

	if err != nil {
		return err
	}

	r.Body = io.NopCloser(bytes.NewReader(body)) // Восстановливаем сразу
	deleteSet := make(map[string]bool, len(fields))

	for _, f := range fields {
		deleteSet[f] = true
	}

	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.SetBoundary(boundary); err != nil {
		return nil
	}

	changed := false

	for {
		part, err := reader.NextPart()

		if err == io.EOF {
			break
		}

		if err != nil {
			return nil
		}

		if deleteSet[part.FormName()] {
			changed = true
			_ = part.Close()
			continue
		}

		hdr := make(textproto.MIMEHeader, len(part.Header))

		for k, vs := range part.Header {
			for _, v := range vs {
				hdr.Add(k, v)
			}
		}

		dst, err := writer.CreatePart(hdr)

		if err != nil {
			_ = part.Close()
			return nil
		}

		if _, err := io.Copy(dst, part); err != nil {
			_ = part.Close()
			return nil
		}

		_ = part.Close()
	}

	if err := writer.Close(); err != nil {
		return nil
	}

	if !changed {
		return nil
	}

	newBody := buf.Bytes()
	r.Body = io.NopCloser(bytes.NewReader(newBody))
	r.ContentLength = int64(len(newBody))
	r.Header.Set("Content-Length", strconv.Itoa(len(newBody)))

	return nil
}
