# syntax=docker/dockerfile:1

FROM node:20-bookworm AS web-builder
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build:embed

FROM golang:1.26-bookworm AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=web-builder /src/src/server/webdist ./src/server/webdist
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags embed_frontend -o /out/lived .

FROM debian:bookworm-slim AS runtime
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=go-builder /out/lived /usr/local/bin/lived
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/lived", "run"]
