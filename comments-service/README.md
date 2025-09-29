# Comments-service

## Содержание
- [Краткое описание](#краткое-описание)
- [Архитектура сервиса](#архитектура-сервиса)
- [API (gRPC)](#api-grpc)
- [Конфигурация](#конфигурация)
- [БД](#бд-и-миграции)
- [Безопасность](#безопасность)
- [Логирование и диагностика](#логирование-и-диагностика)
- [Тесты, CI/CD](#тесты-cicd)

---

## Краткое описание

**Comments-service** — gRPC-сервис комментариев для «Новостного агрегатора».  
Поддерживает:
- создание корневых комментариев и ответов (дерево через `parent_id`);
- мягкое удаление (маскирование контента при `is_deleted=true`);
- курсорную пагинацию:
  - по новости — корневые, сначала новые;
  - по ветке — ответы одного `parent_id`, сначала старые;
- **TTL веток**: для корня задаётся `expires_at = now + THREAD_TTL`, все ответы наследуют эту дату; очистка обеспечивается TTL-индексом MongoDB;
- хранилище — MongoDB;
- health-probes и метрики Prometheus.

---

## Архитектура сервиса

### Ключевые аспекты: 
```bash
cmd/comments-service/    # main: точка входа
internal/
  config/                # загрузка конфигурации (cleanenv)
  models/                # доменные модели 
  service/               # бизнес-логика 
  storage/               # интерфейсы хранилища
  storage/mongo/         # реализация Storage на MongoDB
  transport/grpc/        # адаптер к protobuf API (сервер)
gen/go/comments/         # сгенерированные protobuf-типы/клиенты
```
---

## API (gRPC)

### Сервис `comments.CommentsService`

- CreateComment(CreateCommentRequest) -> CreateCommentResponse
Создаёт корень (если parent_id="", требуется news_id) или ответ (если задан parent_id, news_id игнорируется и наследуется от родителя). Возвращает созданный Comment.

- DeleteComment(DeleteCommentRequest) -> DeleteCommentResponse
Мягкое удаление по id (устанавливает is_deleted=true, чистит content).

- CommentByID(CommentByIDRequest) -> CommentByIDResponse
Возвращает один Comment по строковому id.

- ListByNews(ListByNewsRequest) -> ListByNewsResponse
Страница корневых комментариев новости (сначала новые). Возвращает comments[] и next_page_token.

- ListReplies(ListRepliesRequest) -> ListRepliesResponse
Страница ответов в пределах одной ветки (parent_id), сначала старые. Возвращает comments[] и next_page_token.

Proto‑схемы лежат в `comments.proto`, сгенерированные типы — в `gen/go/comments`.

### Маппинг ошибок

- ErrInvalidArgument / ErrInvalidCursor -> InvalidArgument
- ErrNotFound / ErrParentNotFound -> NotFound
- ErrConflict -> AlreadyExists
- ErrThreadExpired / ErrMaxDepthExceeded -> FailedPrecondition
- прочее -> Internal

---

## Конфигурация 

Загрузка с предсказуемым приоритетом источников:
1. явный путь `--config`,
2. переменная окружения `CONFIG_PATH`,
3. `./local.yaml` в рабочей директории,
4. переменные окружения (через `cleanenv`).

### Переменные окружения

env: local | dev | prod

grpc:
  host: "0.0.0.0"      # ENV: GRPC_HOST, default: 0.0.0.0
  port: "50054"        # ENV: GRPC_PORT, default: 50054

http:
  host: "0.0.0.0"      # ENV: HTTP_HOST, default: 0.0.0.0
  port: "50084"        # ENV: HTTP_PORT, default: 50084

db:
  url: "mongodb://administrator:administrator@comments-db:27017/comments"    # ENV: DATABASE_URL (required)

limits:
  default:  20          # размер страницы по умолчанию
  max:      100         # кап размера страницы
  max_depth: 3          # максимальная глубина ветки (0 — корень)

ttl:
  thread: "168h"        # срок жизни ветки (корня); ответы наследуют его

timeouts:
  service: "5s"         # общий таймаут на обработку запроса


| Ключ           | Описание                          | Значение по умолчанию |
| -------------- | --------------------------------- | --------------------- |
| `ENV`          | окружение (`local/dev/prod`)      | `local`               |
| `GRPC_HOST`    | адрес gRPC-сервера                | `0.0.0.0`             |
| `GRPC_PORT`    | порт gRPC-сервера                 | `50054`               |
| `HTTP_HOST`    | адрес HTTP-пробок/метрик          | `0.0.0.0`             |
| `HTTP_PORT`    | порт HTTP-пробок/метрик           | `50084`               |
| `DATABASE_URL` | строка подключения MongoDB        | **(обязателен)**      |
| `THREAD_TTL`   | TTL ветки (например `168h`)       | `168h`                |
| `SERVICE`      | сервисный таймаут (например `5s`) | `5s`                  |

---

## БД

Хранилище — MongoDB. Миграции в классическом смысле не требуются.

При старте создаются индексы:
- TTL по expires_at (очистка просроченных веток),
- news_id,parent_id,created_at(desc) — листинг корней новости,
- parent_id,created_at(asc) — листинг ответов ветки.

Имя БД берётся из пути URI (mongodb://host:27017/<dbName>). Если путь не задан — используется comments.

---

## Безопасность 

- Сервис не хранит секреты; строка подключения к БД должна приходить из окружения/секрет-менеджера.
- В продакшене рекомендуется включать аутентификацию MongoDB и использовать отдельного пользователя/роль только на свою БД.

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
GO_TEST_INTEGRATION=1 go test ./internal/storage/mongo -v -race -count=1
```

### CI/CD (GitHub Actions)
- **CI**: линт/тесты/сборка (Go 1.24), где интеграционные тесты c testcontainers.
- **CD**: multi‑arch образ публикуется в GHCR как `ghcr.io/<owner>/go-news-aggregator/comments-service` с тегами `latest`, `v*` и `sha` (см. `.github/workflows/comments-cd.yml`).

---
