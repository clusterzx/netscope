# syntax=docker/dockerfile:1.7
# NetScope: one static Go binary with the embedded web UI, on Alpine with nmap/arp-scan.

# ---- 1. web UI (SvelteKit, static adapter) ----
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json web/.npmrc ./
RUN npm ci
COPY web/ ./
RUN mkdir -p /src/internal/webui/dist && npm run build

# ---- 2. Go binary (no CGO) ----
FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "-s -w -X main.version=${VERSION}" -o /out/netscope ./cmd/netscope

# ---- 3. runtime ----
FROM alpine:3.22
RUN apk add --no-cache nmap nmap-scripts arp-scan ca-certificates tzdata \
 && mkdir -p /data
COPY --from=build /out/netscope /usr/local/bin/netscope
ENV NETSCOPE_DATA_DIR=/data TZ=Europe/Berlin
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 CMD ["netscope", "healthcheck"]
ENTRYPOINT ["netscope"]
CMD ["serve"]
