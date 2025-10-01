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
COPY api-gateway/go.mod api-gateway/go.sum ./api-gateway/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd ./api-gateway && go mod download

# Теперь копируем остальной репозиторий
COPY . .

# Статическая сборка бинарника под целевую платформу
# ВАЖНО: Buildx подставит значения ARG, а мы передаём их в GOOS/GOARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -buildvcs=false -ldflags="-s -w" \
      -o /out/api-gateway ./api-gateway/cmd/api-gateway


# Runtime (non-root)
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

LABEL org.opencontainers.image.title="api-gateway" \
      org.opencontainers.image.source="https://github.com/pribylovaa/go-news-aggregator"

COPY --from=builder /out/api-gateway /usr/local/bin/api-gateway

ENV CONFIG_PATH=/etc/api-gateway/config.yaml

EXPOSE 50090
EXPOSE 50085

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/api-gateway"]