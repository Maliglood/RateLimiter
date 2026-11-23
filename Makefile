# Makefile

# --- Переменные ---
# Бинарник линтера будет установлен в локальную папку ./bin
GOLANGCI_LINT := $(shell pwd)/bin/golangci-lint
# Имя итогового бинарного файла
BINARY_NAME=rate-limiter

# --- Цели ---

.PHONY: help
help: ## Показать это сообщение
	@echo "Доступные команды:"
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: test
test: ## Запустить тесты с детектором гонок и покрытием
	go test -race -cover -count=1 ./...

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Запустить линтер (golangci-lint)
	@echo "Запуск линтера..."
	@$(GOLANGCI_LINT) run --timeout=5m

# Правило для установки golangci-lint, если его нет
$(GOLANGCI_LINT):
	@echo "Установка golangci-lint в ./bin..."
	@mkdir -p ./bin
	@GOBIN=$(shell pwd)/bin go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

.PHONY: build
build: ## Собрать бинарный файл для production
	@echo "Сборка $(BINARY_NAME)..."
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/$(BINARY_NAME) ./cmd/server

.PHONY: tidy
tidy: ## Привести go.mod и go.sum в порядок
	go mod tidy -v

.PHONY: vet
vet: ## Запустить go vet
	go vet ./...

.PHONY: all
all: tidy vet test lint build ## Запустить все основные проверки и сборку

.PHONY: fmt-check
fmt-check: ## Проверить, что все файлы отформатированы (для CI)
	@files="$$(gofmt -l ./cmd ./internal)"; \
	if [ -n "$$files" ]; then \
		echo "Не отформатированы:"; echo "$$files"; exit 1; \
	fi