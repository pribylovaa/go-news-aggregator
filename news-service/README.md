# News-service

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

**News-service** — gRPC-сервис для сбора и выдачи новостной ленты.  
Сервис периодически опрашивает RSS-источники, нормализует записи и сохраняет их в PostgreSQL. Внешний API предоставляет постраничную ленту и получение записи по ID. В комплекте — health‑check и базовая наблюдаемость (структурные логи, recover, таймауты).

---

## Архитектура сервиса

### Основные компоненты:
```bash
cmd/news-service/main.go    # точка входа
internal/
    config/                 # загрузка/валидация конфигурации (cleanenv)
    models/                 # доменные модели 
    service/                # бизнес-логика: ListNews, NewsByID, ingest-цикл (оркестрация парсера и хранилища)
    rss/                    # Parser для RSS 2.0: нормализация ссылок/дат/описаний
    storage/                # контракты доступа к БД (интерфейсы, ошибки)
    storage/postgres/       # реализация на PostgreSQL
    transport/grpc/         # серверная реализация protobuf API + маппинг ошибок
gen/go/news/                # сгенерированные go-типы (newsv1)
migrations/                 # миграции
```

### Ключевые решения

- Пагинация — keyset по (published_at DESC, id DESC) с непрозрачным page_token (base64url).
- Upsert-политика — уникальность по link; title обновляется всегда; image_url/category/short_description — только если пришли непустые; long_description — если новая длиннее текущей; published_at неизменен; fetched_at всегда обновляется.
- Ingest — конкурентный парсинг источников, доведение инвариантов (UTC, заполнение описаний и дат), сохранение батчем.

---

## API (gRPC)

### Сервис `news.NewsService`

Методы:
```bash
rpc ListNews (ListNewsRequest)   returns (ListNewsResponse);
rpc NewsByID (NewsByIDRequest)   returns (NewsByIDResponse);
```

Сообщение News:
```bash
message News {
  string id                = 1;   // UUID
  string title             = 2;
  string category          = 3;
  string short_description = 4;
  string long_description  = 5;
  string link              = 6;   // URL, уникальный
  string image_url         = 7;
  int64  published_at      = 8;   // unix (UTC)
  int64  fetched_at        = 9;   // unix (UTC)
}
```

Маппинг ошибок:
- InvalidArgument — битый или чужой page_token (курсор).
- NotFound — запись отсутствует.
- Internal — прочие ошибки сервиса/хранилища (без утечки деталей).

---

## Конфигурация 

Загрузка с предсказуемым приоритетом источников:
1. явный путь `--config`,
2. переменная окружения `CONFIG_PATH`,
3. `./local.yaml` в рабочей директории,
4. переменные окружения (через `cleanenv`).

### Переменные окружения

| YAML               | ENV                 | По умолчанию |
| ------------------ | ------------------- | ------------ |
| `env`              | `ENV`               | `local`      |
| `grpc.host`        | `GRPC_HOST`         | `0.0.0.0`    |
| `grpc.port`        | `GRPC_PORT`         | `50052`      |
| `db.url`           | `DATABASE_URL`      | **required** |
| `fetcher.sources`  | `RSS_SOURCES` (CSV) | —            |
| `fetcher.interval` | `FETCH_INTERVAL`    | `10m` (≥ 1m) |
| `limits.default`   | `DEFAULT_LIMIT`     | `12`         |
| `limits.max`       | `MAX_LIMIT`         | `300`        |
| `timeouts.service` | `SERVICE`           | `5s`         |

---

## БД и миграции

Расширения: pgcrypto, citext.

Таблица news:
id uuid PK DEFAULT gen_random_uuid()
title text NOT NULL
category text NOT NULL DEFAULT ''
short_description text NOT NULL DEFAULT ''
long_description text NOT NULL DEFAULT ''
link CITEXT UNIQUE NOT NULL
image_url text NOT NULL DEFAULT ''
published_at timestamptz NOT NULL DEFAULT now()
fetched_at timestamptz NOT NULL DEFAULT now()

Индекс: ix_news_published_id_desc (published_at DESC, id DESC).

Миграции: migrations/1_init_news.up.sql, migrations/1_init_news.down.sql.

---

## Безопасность 

- Сервис читает публичные RSS-источники и предоставляет read-only API.
- Аутентификация/авторизация прикрываются на уровне api-gateway; прямой доступ к gRPC из внешней сети не предполагается.
- Логи не содержат чувствительных данных (заголовок/URL новости и служебные поля).

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
- **CD**: multi‑arch образ публикуется в GHCR как `ghcr.io/<owner>/go-news-aggregator/news-service` с тегами `latest`, `v*` и `sha` (см. `.github/workflows/news-cd.yml`).

---