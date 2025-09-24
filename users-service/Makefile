# Пути и имена
SERVICE   := users-service
CMD_DIR   := ./cmd/$(SERVICE)
BIN_DIR   := ./bin
BIN       := $(BIN_DIR)/$(SERVICE)

# Compose файлы и конфиги 
COMPOSE_LOCAL := docker-compose.local.yml
COMPOSE_DEV   := docker-compose.dev.yml
CONFIG_LOCAL  := ./config/local.yaml
CONFIG_DEV    := ./config/dev.yaml

# Цели по умолчанию
.PHONY: help
help:
	@echo "Основные цели:"
	@echo "  lint                  - запустить golangci-lint"
	@echo "  test                  - юнит-тесты"
	@echo "  test-integration      - интеграционные тесты"
	@echo "  build                 - собрать бинарник в $(BIN)"
	@echo "  run-local             - запустить локально с $(CONFIG_LOCAL)"
	@echo "  run-dev               - запустить локально с $(CONFIG_DEV)"
	@echo "  docker-build          - собрать Docker-образ news-service:local"
	@echo "  up-local / down-local - поднять/остановить стек из $(COMPOSE_LOCAL)"
	@echo "  up-dev   / down-dev   - поднять/остановить стек из $(COMPOSE_DEV)"
	@echo "  logs-local / logs-dev - логи сервиса из соответствующего стека"
	@echo "  migrate-local         - применить миграции через compose (local)"
	@echo "  migrate-dev           - применить миграции через compose (dev)"
	@echo "  net-create            - создать external-сеть go-news-net (для dev)"
	@echo "  clean                 - удалить каталог $(BIN_DIR)"

# Качество 
.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: test
test:
	go test -race -count=1 ./...

.PHONY: test-integration
test-integration:
	GO_TEST_INTEGRATION=1 go test -v -race -count=1 ./internal/storage/postgres

# Сборка/запуск без Docker 
.PHONY: build
build:
	mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -tags timetzdata -ldflags "-s -w" -o $(BIN) $(CMD_DIR)

.PHONY: run-local
run-local:
	go run $(CMD_DIR) -config $(CONFIG_LOCAL)

.PHONY: run-dev
run-dev:
	go run $(CMD_DIR) -config $(CONFIG_DEV)

# Docker 
.PHONY: docker-build
docker-build:
	docker build -t $(SERVICE):local -f Dockerfile ..

# Compose: local 
.PHONY: up-local
up-local:
	docker compose -f $(COMPOSE_LOCAL) up -d --build

.PHONY: down-local
down-local:
	docker compose -f $(COMPOSE_LOCAL) down -v

.PHONY: logs-local
logs-local:
	docker compose -f $(COMPOSE_LOCAL) logs -f $(SERVICE)

.PHONY: migrate-local
migrate-local:
	docker compose -f $(COMPOSE_LOCAL) up --build --exit-code-from users-migrate users-migrate

# Compose: dev 
.PHONY: net-create
net-create:
	-@docker network create go-news-net 2>/dev/null || true

.PHONY: up-dev
up-dev: net-create
	docker compose -f $(COMPOSE_DEV) up -d --build

.PHONY: down-dev
down-dev:
	docker compose -f $(COMPOSE_DEV) down 

.PHONY: logs-dev
logs-dev:
	docker compose -f $(COMPOSE_DEV) logs -f $(SERVICE)

.PHONY: migrate-dev
migrate-dev:
	docker compose -f $(COMPOSE_DEV) up --build --exit-code-from users-migrate users-migrate

# Прочее
.PHONY: clean
clean:
	rm -rf $(BIN_DIR)