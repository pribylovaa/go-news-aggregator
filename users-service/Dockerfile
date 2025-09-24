# Builder 
FROM golang:1.24-bookworm AS builder 

WORKDIR /src 

COPY users-service/go.mod users-service/go.sum ./users-service/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd ./users-service && go mod download

COPY . . 

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/users-service ./users-service/cmd/users-service

# Runtime (non-root)

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

LABEL org.opencontainers.image.title="users-service" \
      org.opencontainers.image.source="https://github.com/pribylovaa/go-news-aggregator"

COPY --from=builder /out/users-service /usr/local/bin/users-service

ENV CONFIG_PATH=/etc/users-service/config.yaml

EXPOSE 50053
EXPOSE 50083
USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/users-service"]