# Builder
FROM golang:1.24-alpine AS builder

WORKDIR /src
RUN apk add --no-cache ca-certificates tzdata git

# Кэш зависимостей
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

# Исходники
COPY . .

# Сборка статического бинарника
ENV CGO_ENABLED=0
# Путь до main-пакета
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -tags timetzdata -ldflags "-s -w" -o /out/auth-service ./cmd/auth-service

# Runtime
FROM gcr.io/distroless/static:nonroot AS runtime

WORKDIR /app

# Бинарник
COPY --from=builder /out/auth-service /usr/local/bin/auth-service
# CA-сертификаты (на случай исходящего TLS)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Конфиг по умолчанию внутри контейнера
ENV CONFIG_PATH=/etc/auth-service/config.yaml

USER nonroot:nonroot
EXPOSE 50051

ENTRYPOINT ["/usr/local/bin/auth-service"]
