# The frontend is built first so it can be embedded into the Go binary.
FROM node:22-alpine AS web
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Overwrite the placeholder dist with the real build from the previous stage.
COPY --from=web /app/web/dist ./web/dist
# CGO stays off: modernc.org/sqlite is pure Go, so the result is a static binary.
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /roundly ./cmd/server

FROM alpine:3.20
# ca-certificates is required to reach Google's OAuth endpoints over TLS.
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 roundly \
    && mkdir -p /data \
    && chown roundly:roundly /data

COPY --from=build /roundly /usr/local/bin/roundly

USER roundly
WORKDIR /data
# The SQLite file lives on a volume so it survives container replacement.
VOLUME ["/data"]
EXPOSE 8080

ENV ADDR=:8080 \
    ENV=production \
    DATABASE_URL=/data/roundly.db

ENTRYPOINT ["/usr/local/bin/roundly"]
