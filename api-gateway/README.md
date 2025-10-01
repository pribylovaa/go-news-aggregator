# Api-Gateway 

## Что внутри?

- REST поверх gRPC: конвертация DTO <-> proto, вызовы апстримов через клиентские интерсепторы.
- Единый формат ошибок: { "error": { "code", "message", "request_id" } }.
- Middleware: Recover, RequestID, Logging (через pkg/log), AuthBearer, Timeout.
- Метрики/пробы: отдельный HTTP на :50085 с /metrics, /livez, /healthz.
- Чистый логгер: slog + pkg/log (request-scoped logger в контексте).

---

## Архитектура

```bash
[ Client ] --HTTP--> [ API Gateway ]
                         | (gRPC)
             +-----------+-----------+-----------+
             |           |           |           |
          [auth]      [news]     [comments]   [users]
```

---

## Структура проекта

### Основные пакеты:
```bash
api-gateway/
├─ cmd/api-gateway/           # main, запуск двух HTTP-серверов (API + metrics)
├─ internal/
│  ├─ http/
│  │  ├─ handlers/           # REST-хендлеры (auth/news/comments/users)
│  │  ├─ middleware/         # RequestID/AuthBearer/Timeout/Recover/Logging + tests
│  │  └─ router.go           # chi + регистрация маршрутов, BasePath
│  ├─ clients/               # gRPC-клиенты апстримов (auth/news/comments/users)
│  ├─ config/ 
│  ├─ models/                # DTO и convert.go (REST <-> proto)
│  └─ errors/                # gRPC -> HTTP ошибки, WriteError()
├─ config/
│  ├─ local.yaml
│  └─ dev.yaml
└─ Dockerfile
```

---

## Конфигурация

### Загрузка в порядке приоритета:
- --config
- CONFIG_PATH
- ./local.yaml
- только ENV

### Структура config.Config:

```bash
env: local|dev|prod

http:
  host: 0.0.0.0
  port: "50090"        # REST/API

metrics:
  host: 0.0.0.0
  port: "50085"        # /metrics, /livez, /healthz

timeouts:
  service: 15s         # общий таймаут обработки HTTP-запроса

grpc:
  auth_addr: "0.0.0.0:50081"
  news_addr: "0.0.0.0:50082"
  users_addr: "0.0.0.0:50083"
  comments_addr: "0.0.0.0:50084"
```

---

## HTTP-маршруты (REST)

- Базовый префикс (если включён): /api.

### Auth
```bash
POST   /auth/register
POST   /auth/login
POST   /auth/refresh
POST   /auth/revoke
POST   /auth/validate
```

### News
```bash
GET    /news                ?limit=&page_token=
GET    /news/{id}
```

### Comments
```bash
POST   /comments
GET    /comments/{id}
GET    /news/{news_id}/comments    ?page_size=&page_token=
GET    /comments/{id}/replies      ?page_size=&page_token=
```

### Users
```bash
GET    /users/{id}
PATCH  /users/{id}
POST   /users/{id}/avatar/presign
POST   /users/{id}/avatar/confirm
```

---

## Маппинг ошибок 

Единый JSON-ответ, маппинг gRPC -> HTTP:

```bash
{
  "error": {
    "code": "invalid_argument | not_found | already_exists | failed_precondition | unauthenticated | ...",
    "message": "короткое безопасное описание",
    "request_id": "uuid/hex"   // если есть X-Request-Id
  }
}
```
### Основные соответствия:
- InvalidArgument - 400;
- NotFound - 404;
- AlreadyExists - 409;
- FailedPrecondition - 412;
- Unauthenticated - 401;
- PermissionDenied - 403;
- ResourceExhausted - 429;
- Canceled - 499;
- DeadlineExceeded - 504;
- Unavailable - 503;
- прочее - 500.

---

## Наблюдаемость

- Метрики Prometheus: GET /metrics (на metrics-сервере, порт 50085).
- Liveness: GET /livez -> 200 ok.
- Readiness: GET /healthz -> 200 ok после успешного старта серверов/бинда портов и инициализации клиентов, иначе 503 not ready.
- Логи: JSON (для dev/prod) или text (для local), поля уровня/времени/атрибутов совместимы со стандартным slog.
