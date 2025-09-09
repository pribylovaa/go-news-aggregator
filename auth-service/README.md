# auth-service

Содержание:

## Содержание
- [Краткое описание](#краткое-описание)
- [Архитектура сервиса](#архитектура-сервиса)
- [API (gRPC)](#api-grpc)
- [Конфигурация](#конфигурация)
- [БД и миграции](#бд-и-миграции)
- [Безопасность](#безопасность)
- [Логирование и диагностика](#логирование-и-диагностика)
- [Тесты, CI/CD](#тесты-cicd)

---

## Краткое описание

**Auth-service** — gRPC‑сервис аутентификации и авторизации пользователей: регистрация и логин пользователей, выпуск/валидация **JWT** access‑токенов, ротация/отзыв refresh‑токенов, health‑check и базовая наблюдаемость (структурные логи, recover, таймауты).

---

## Архитектура сервиса

### Ключевые аспекты: 

cmd/auth-service/        # main: точка входа
internal/
  config/                # загрузка конфигурации (cleanenv)
  interceptors/          # gRPC-интерсепторы: logging, recover, timeout
  models/                # доменные модели 
  pkg/log, pkg/redact    # утилиты логирования и маскировки секретов
  service/               # бизнес-логика 
  storage/               # интерфейсы хранилища
  storage/postgres/      # реализация Storage на PostgreSQL (pgxpool)
  transport/grpc/        # адаптер к protobuf API (сервер)
gen/go/auth/             # сгенерированные protobuf-типы/клиенты
migrations/              # SQL-миграции (users, refresh_tokens)

---

## API (gRPC)

### Сервис `auth.AuthService`

Методы:
- `RegisterUser(RegisterRequest) -> AuthResponse`
- `LoginUser(LoginRequest) -> AuthResponse`
- `RefreshToken(RefreshTokenRequest) -> AuthResponse` *(ротация refresh-токена)*
- `RevokeToken(RevokeTokenRequest) -> RevokeTokenResponse` *(logout)*
- `ValidateToken(ValidateTokenRequest) -> ValidateTokenResponse` *(невалидность возвращается как `valid=false`, а не RPC‑ошибкой)*

Proto‑схемы лежат в `auth.proto`, сгенерированные типы — в `gen/go/auth`.

### Маппинг ошибок

InvalidCredentials / InvalidToken / TokenExpired / TokenRevoked -> Unauthenticated
EmailTaken                                                      -> AlreadyExists
InvalidEmail / WeakPassword / EmptyPassword                     -> InvalidArgument

---

## Конфигурация 

Загрузка с предсказуемым приоритетом источников:
1. явный путь `--config`,
2. переменная окружения `CONFIG_PATH`,
3. `./local.yaml` в рабочей директории,
4. переменные окружения (через `cleanenv`).

### Переменные окружения

| YAML                     | ENV                 | По умолчанию     |
|--------------------------|---------------------|------------------|
| `env`                    | `ENV`               | `local`          |
| `grpc.host`              | `HOST`              | `0.0.0.0`        |
| `grpc.port`              | `PORT`              | `50051`          |
| `auth.jwt_secret`        | `JWT_SECRET`        | **required**     |
| `auth.access_token_ttl`  | `ACCESS_TOKEN_TTL`  | `15m`            |
| `auth.refresh_token_ttl` | `REFRESH_TOKEN_TTL` | `720h`           |
| `auth.issuer`            | `ISSUER`            | `auth-service`   |
| `auth.audience`          | `AUDIENCE`          | `api-gateway`    |
| `db.db_url`              | `DATABASE_URL`      | **required**     |
| `timeouts.service`       | `SERVICE`           | `5s`             |

---

## БД и миграции

Таблицы:
- `users` — уникальный `email` (CITEXT), `password_hash`, временные метки (created_at и updated_at).
- `refresh_tokens` — `token_hash` (уникальный), `user_id` (FK), временные метки (created_at и expires_at), `revoked` + индексы по `user_id` и `expires_at`.

Миграции находятся в `./migrations` и автоматически применяются сервисом `auth-migrate` в Docker Compose.

---

## Безопасность 

- **Access‑JWT**: HS256; кастомные claim’ы `uid`, `email` + стандартные `iss/sub/aud/iat/exp`; строгая проверка алгоритма, issuer/audience и истечения (5s leeway). 
- **Refresh‑токены**: плейн‑значение отдаётся клиенту, в БД хранится только **SHA‑256** хэш (base64url, без паддинга); при ротации старый токен немедленно помечается как `revoked`.
- **Пароли**: хранение только в виде хэша; политики валидации проверяются на уровне сервиса.
- **Маскировка секретов в логах**: утилиты `redact.Email`, `redact.Token`, `redact.Password` исключают утечки чувствительных данных.

---

## Логирование и диагностика

- Unary‑интерсептор логирует по схеме: `method`, `peer`, `request_id`, код ответа и длительность. 
- Recover‑интерсептор конвертирует панику в `codes.Internal` и логирует стек.
- Интерсептор таймаута навешивает дедлайн, если он не задан клиентом.
- В `local/dev` включена gRPC‑reflection, доступен стандартный health‑check.

---

## Тесты, CI/CD

### Тесты 

```bash
# Юнит-тесты по всем пакетам.
go test ./... -race -count=1

# Интеграционные тесты PostgreSQL (testcontainers-go).
GO_TEST_INTEGRATION=1 go test ./internal/storage/postgres -v -race -count=1
```

### CI/CD (GitHub Actions)
- **CI**: линт/тесты/сборка (Go 1.24), где интеграционные тесты c testcontainers.
- **CD**: multi‑arch образ публикуется в GHCR как `ghcr.io/<owner>/go-news-aggregator/auth-service` с тегами `latest`, `v*` и `sha` (см. `.github/workflows/auth-cd.yml`).

---