# The frontend build output is vendored in internal/server/dist, so the
# image builds from Go sources alone — no node/npm stage. Run
# `make frontend` (and commit the regenerated dist) after changing
# frontend sources.

FROM golang:1.26-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X gopad/internal/server.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /gopad ./cmd/gopad

FROM alpine:3.20
RUN adduser -D -u 1000 gopad && mkdir /data && chown gopad /data
USER gopad
COPY --from=backend /gopad /usr/local/bin/gopad
ENV PORT=3030 SQLITE_PATH=/data/gopad.db
VOLUME /data
EXPOSE 3030
ENTRYPOINT ["gopad"]
