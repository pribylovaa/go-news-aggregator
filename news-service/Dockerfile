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
COPY news-service/go.mod news-service/go.sum ./news-service/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd ./news-service && go mod download

# Теперь копируем остальной репозиторий
COPY . .

# Статическая сборка бинарника под целевую платформу
# ВАЖНО: Buildx подставит значения ARG, а мы передаём их в GOOS/GOARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -buildvcs=false -ldflags="-s -w" \
      -o /out/news-service ./news-service/cmd/news-service


# Runtime (non-root)
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

LABEL org.opencontainers.image.title="news-service" \
      org.opencontainers.image.source="https://github.com/pribylovaa/go-news-aggregator"

COPY --from=builder /out/news-service /usr/local/bin/news-service

ENV CONFIG_PATH=/etc/news-service/config.yaml

EXPOSE 50052
EXPOSE 50082

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/news-service"]

