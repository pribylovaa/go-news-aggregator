# syntax=docker/dockerfile:1.7

############################
# Builder
############################
FROM golang:1.24-bookworm AS builder

# Эти ARG автоматически пробрасываются Buildx при сборке под разные платформы
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

WORKDIR /src

# Оптимизация кеша модулей: сначала переносим только go.mod/go.sum,
# чтобы go mod download кешировался отдельно от исходников.
COPY auth-service/go.mod auth-service/go.sum ./auth-service/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd ./auth-service && go mod download

# Теперь копируем остальной репозиторий
COPY . .

# Статическая сборка бинарника под целевую платформу
# ВАЖНО: Buildx подставит значения ARG, а мы передаём их в GOOS/GOARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -buildvcs=false -ldflags="-s -w" \
      -o /out/auth-service ./auth-service/cmd/auth-service

############################
# Runtime (non-root)
############################
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

LABEL org.opencontainers.image.title="auth-service" \
      org.opencontainers.image.source="https://github.com/pribylovaa/go-news-aggregator"

COPY --from=builder /out/auth-service /usr/local/bin/auth-service

ENV CONFIG_PATH=/etc/auth-service/config.yaml

EXPOSE 50051
EXPOSE 50081

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/auth-service"]
