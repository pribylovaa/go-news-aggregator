# Builder 
FROM golang:1.24-bookworm AS builder 

WORKDIR /src 

COPY news-service/go.mod news-service/go.sum ./news-service/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd ./news-service && go mod download

COPY . . 

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/news-service ./news-service/cmd/news-service

# Runtime (non-root)

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="news-service" \
      org.opencontainers.image.source="https://github.com/pribylovaa/go-news-aggregator"

COPY --from=builder /out/news-service /usr/local/bin/news-service

ENV CONFIG_PATH=/etc/news-service/config.yaml

EXPOSE 50051
USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/news-service"]

