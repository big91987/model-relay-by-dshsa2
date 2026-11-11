# syntax=docker/dockerfile:1

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.25-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H -u 10001 gateway
WORKDIR /app
COPY --from=server /out/gateway /app/gateway
COPY --from=web /src/web/dist /app/web/dist
RUN mkdir -p /app/data && chown -R gateway /app
USER gateway
ENV PORT=8080 \
    DATABASE_PATH=/app/data/gateway.db \
    WEB_DIST=/app/web/dist
EXPOSE 8080
VOLUME ["/app/data"]
ENTRYPOINT ["/app/gateway"]
