# Builder 
FROM golang:1.24-bookworm AS builder 

WORKDIR /src 

COPY auth-service/go.mod auth-service/go.sum ./auth-service/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd ./auth-service && go mod download

COPY . . 

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/auth-service ./auth-service/cmd/auth-service

# Runtime (non-root)

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="auth-service" \
      org.opencontainers.image.source="https://github.com/pribylovaa/go-news-aggregator"

COPY --from=builder /out/auth-service /usr/local/bin/auth-service

ENV CONFIG_PATH=/etc/auth-service/config.yaml

EXPOSE 50051
USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/auth-service"]

