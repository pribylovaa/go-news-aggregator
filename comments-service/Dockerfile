# syntax=docker/dockerfile:1.7
# Builder
FROM golang:1.24-bookworm AS builder

# Эти ARG автоматически пробрасываются Buildx при сборке под разные платформы
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

WORKDIR /src

# Оптимизация кеша модулей: сначала переносим только go.mod/go.sum,
# чтобы go mod download кешировался отдельно от исходников.
COPY comments-service/go.mod comments-service/go.sum ./comments-service/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd ./comments-service && go mod download

# Теперь копируем остальной репозиторий
COPY . .

# Статическая сборка бинарника под целевую платформу
# ВАЖНО: Buildx подставит значения ARG, а мы передаём их в GOOS/GOARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -buildvcs=false -ldflags="-s -w" \
      -o /out/comments-service ./comments-service/cmd/comments-service

# Runtime (non-root)
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

LABEL org.opencontainers.image.title="comments-service" \
      org.opencontainers.image.source="https://github.com/pribylovaa/go-news-aggregator"

COPY --from=builder /out/comments-service /usr/local/bin/comments-service

ENV CONFIG_PATH=/etc/comments-service/config.yaml

EXPOSE 50054
EXPOSE 50084

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/comments-service"]
