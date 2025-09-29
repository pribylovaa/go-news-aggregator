# Users-service

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

**Users-service** — gRPC-сервис для управления профилями и аватарами пользователей.
Профили хранятся в PostgreSQL, файлы аватаров — в MinIO/S3. Внешний API даёт чтение профиля по ID, создание, частичное обновление с mask и работу с аватаром: выдача presigned URL для загрузки и подтверждение загрузки. В комплекте — health-check и базовая наблюдаемость (структурные логи, перехват паник, таймауты).

---

## Архитектура сервиса

### Основные компоненты:
```bash
cmd/users-service/main.go    # точка входа
internal/
    config/                  # загрузка и валидация конфигурации (cleanenv)
    models/                  # доменные модели (Profile, Gender)
    service/                 # бизнес-логика: CreateProfile, ProfileByID, UpdateProfile (mask), аватар (presign/confirm)
    storage/                 # контракты хранилищ (интерфейсы, ошибки)
    storage/postgres/        # реализация профилей на PostgreSQL (CRUD, маппинг SQL-ошибок)
    storage/minio/           # работа с аватарами в MinIO/S3 (presigned URL, policy)
    transport/grpc/          # сервер protobuf API + маппинг ошибок/enum
gen/go/users/                # сгенерированные go-типы (usersv1)
migrations/                  # миграции БД (profiles)
```

### Ключевые решения

- Частичное обновление — через FieldMask: изменяются только указанные поля; пустые значения применяются только если поле есть в маске. 
- Аватары — presigned PUT в MinIO: проверяет content_type/content_length, формирует ключ profiles/{user_id}/avatar, TTL из конфига; после загрузки — Confirm фиксирует avatar_key/avatar_url в профиле.

---

## API (gRPC)

### Сервис `news.NewsService`

Методы:
```bash
rpc ProfileByID            (ProfileByIDRequest)         returns (Profile);
rpc CreateProfile          (CreateProfileRequest)       returns (Profile);
rpc UpdateProfile          (UpdateProfileRequest)       returns (Profile);
rpc AvatarUploadURL        (AvatarUploadURLRequest)     returns (AvatarUploadURLResponse);
rpc ConfirmAvatarUpload    (ConfirmAvatarUploadRequest) returns (Profile);
```

Маппинг ошибок:
- InvalidArgument — неверный user_id (UUID), некорректные поля (age, gender, content_type/content_length), некорректная/пустая update_mask.
- AlreadyExists — конфликт уникальности профиля/username при создании.
- NotFound — профиль не найден; при подтверждении аватара — объект отсутствует.
- Internal — прочие ошибки сервиса/хранилища/S3 (без утечки деталей).

---

## Конфигурация 

Загрузка с предсказуемым приоритетом источников:
1. явный путь `--config`,
2. переменная окружения `CONFIG_PATH`,
3. `./local.yaml` в рабочей директории,
4. переменные окружения (через `cleanenv`).

### Переменные окружения

| YAML                           | ENV                                  | По умолчанию           |
| ------------------------------ | ------------------------------------ | ---------------------- |
| `env`                          | `ENV`                                | `local`                |
| `grpc.host`                    | `GRPC_HOST`                          | `0.0.0.0`              |
| `grpc.port`                    | `GRPR_PORT`                          | `50053`                |
| `http.host`                    | `HTTP_HOST`                          | `0.0.0.0`              |
| `http.port`                    | `HTTP_PORT`                          | `50083`                |
| `postgres.url`                 | `POSTGRES`                           | **required**           |
| `s3.endpoint`                  | `S3_ENDPOINT`                        | **required**           |
| `s3.root_user`                 | `S3_ROOT_USER`                       | **required**           |
| `s3.root_password`             | `S3_ROOT_PASSWORD`                   | **required**           |
| `s3.bucket`                    | `S3_BUCKET`                          | **required**           |
| `s3.presign_ttl`               | `S3_PRESIGN_TTL`                     | `10m` (≥ 0)            |
| `s3.public_base_url`           | `S3_PUBLIC_BASE_URL`                 | `""` (пусто)           |
| `avatar.max_size_bytes`        | `AVATAR_MAX_SIZE_BYTES`              | `5242880` (5 MiB, ≥ 0) |
| `avatar.allowed_content_types` | `AVATAR_ALLOWED_CONTENT_TYPES` (CSV) | `image/jpeg,image/png` |
| `timeouts.service`             | `SERVICE_TIMEOUT`                    | `5s`                   |

Примечания:
- avatar.allowed_content_types читается как CSV-строка и разбирается по запятой.
- s3.public_base_url опционален; если задан, формирует публичные ссылки на аватар при подтверждении загрузки.

---

## БД и миграции

### PostgreSQL

Расширения: citext

Таблица profiles:
```bash
  user_id     UUID PRIMARY KEY, # внешний идентификатор пользователя
  username    TEXT NOT NULL, # уникальный логин без учета регистра
  age         INT  NOT NULL DEFAULT 0,
  country     TEXT NOT NULL DEFAULT '',
  gender      SMALLINT NOT NULL DEFAULT 0, # 0=UNSPECIFIED, 1=MALE, 2=FEMALE, 3=OTHER
  avatar_key  TEXT NOT NULL DEFAULT '',
  avatar_url  TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
```

Миграции: migrations/1_init_users.up.sql, migrations/1_init_users.down.sql.

### MinIO

Bucket и ключи:
- Один bucket: из config.s3.bucket.
- Ключ аватара: profiles/{user_id}/avatar (стабильный; перезаписываем объект при обновлении).
- Публичный URL формируется так: если задан configs3.public_base_url -> avatar_url = <public_base_url>/<bucket>/<key>, иначе храним только avatar_key.

Ограничения/валидация:
- avatar.max_size_bytes — верхняя граница content_length.
- avatar.allowed_content_types — белый список MIME (image/jpeg,image/png по умолчанию).
- TTL ссылки отдаётся в секундах, с защитой от переполнений (uint32).

---

## Безопасность 

- Валидация входа на границе: trim/нормализация строк, whitelist для gender (enum-switch), проверка диапазонов и безопасные приведения типов (без переполнений), content_type — по белому списку, content_length — по лимиту. 
- Presigned-загрузка аватара: bucket приватный, выдаётся короткоживущая PUT-ссылка с обязательными заголовками; ключ предсказуемый (profiles/{user_id}/avatar), но доступ к объектам только по presign или через публичный CDN-базис, если включён. Ссылки/секреты в логи не пишутся.
- Аутентификация/авторизация прикрываются на уровне api-gateway; прямой доступ к gRPC из внешней сети не предполагается.
- Логи и наблюдаемость: структурные логи без PII/секретов (без токенов и presigned URL), recovery-интерцептор, таймауты на RPC и внешние вызовы.

---

## Логирование и диагностика

- Логи — slog (JSON в dev/prod, текст в local), кореляция по контексту.

- Интерсепторы:
    - Recover — перехват паник → Internal.
    - UnaryLogging — структурное логирование вызовов.
    - WithTimeout — навешивает таймаут по конфигу, если клиентом не задан.

- Health-check — grpc.health.v1.Health.

- Reflection — включен в local/dev для удобства отладки.

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
- **CD**: multi‑arch образ публикуется в GHCR как `ghcr.io/<owner>/go-news-aggregator/users-service` с тегами `latest`, `v*` и `sha` (см. `.github/workflows/users-cd.yml`).

---