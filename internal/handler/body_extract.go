// internal/handler/body_extract.go
package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"

	"rate-limiter/internal/config"
)

// Верхняя граница размера тела для буферизации (10 MiB).
const maxBodySize = 10 << 20

// Параметры, извлечённые из тела.
type bodyParams struct {
	URL   string
	Limit string // int из JSON или строка из формы
}

// Извлекает параметры нашего сервиса из тела и восстанавливает остальное Body для Прокси.
// Возвращает пустые строки, если тело не JSON и не форма или параметров нет.
func extractFromBody(request *http.Request) (bodyParams, error) {
	if request.Body == nil || request.Body == http.NoBody {
		return bodyParams{}, nil
	}

	body, err := io.ReadAll(io.LimitReader(request.Body, maxBodySize+1)) // Читаем тело в память и сразу восстанавливаем
	_ = request.Body.Close()

	if err != nil {
		return bodyParams{}, err
	}

	request.Body = io.NopCloser(bytes.NewReader(body))

	if len(body) > maxBodySize {
		return bodyParams{}, nil // слишком большое — не парсим, не трогаем
	}

	mediaType, params, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))

	switch mediaType {
	case "application/json":
		return extractJSON(body)
	case "application/x-www-form-urlencoded":
		return extractForm(body)
	case "multipart/form-data":
		return extractMultipart(body, params["boundary"])
	}

	return bodyParams{}, nil
}

// Извлекает JSON-тело запроса.
func extractJSON(body []byte) (bodyParams, error) {
	var data map[string]any

	if err := json.Unmarshal(body, &data); err != nil {
		return bodyParams{}, err
	}

	var params bodyParams

	if field, ok := data[config.ParamTargetUrl].(string); ok {
		params.URL = field
	}

	if v, ok := data[config.ParamTargetLimit]; ok {
		switch fieldType := v.(type) {
		case float64:
			params.Limit = strconv.FormatInt(int64(fieldType), 10)
		case string:
			params.Limit = fieldType
		}
	}

	return params, nil
}

// Извлекает данные для запроса формой.
func extractForm(body []byte) (bodyParams, error) {
	values, err := url.ParseQuery(string(body))

	if err != nil {
		return bodyParams{}, err
	}

	return bodyParams{
		URL:   values.Get(config.ParamTargetUrl),
		Limit: values.Get(config.ParamTargetLimit),
	}, nil
}

// Извлекает составные данные.
func extractMultipart(body []byte, boundary string) (bodyParams, error) {
	if boundary == "" {
		return bodyParams{}, nil
	}

	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	var params bodyParams

	for {
		part, err := reader.NextPart()

		if err == io.EOF {
			break
		}

		if err != nil {
			return bodyParams{}, err
		}

		if part.FileName() != "" { // Если файл — то пропускаем
			_ = part.Close()
			continue
		}

		switch part.FormName() {
		case config.ParamTargetUrl:
			field, _ := io.ReadAll(part)
			params.URL = string(field)
		case config.ParamTargetLimit:
			field, _ := io.ReadAll(part)
			params.Limit = string(field)
		}

		_ = part.Close()
	}

	return params, nil
}
