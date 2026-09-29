FROM node:22-bookworm-slim AS frontend
WORKDIR /src
COPY package.json package-lock.json vite.config.ts ./
COPY ui ./ui
RUN npm ci && npm run build

FROM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=frontend /src/internal/httpapi/web/site.js ./internal/httpapi/web/site.js
COPY --from=frontend /src/internal/httpapi/web/chunks ./internal/httpapi/web/chunks
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/blog-api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate-json ./cmd/migrate-json \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bootstrap-admin ./cmd/bootstrap-admin \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/blog-worker ./cmd/worker

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin blog \
    && mkdir -p /app/media && chown blog:blog /app/media
COPY --from=build /out/ /usr/local/bin/
USER blog
ENV BLOG_ADDR=0.0.0.0:8080 BLOG_MEDIA_DIR=/app/media
EXPOSE 8080
CMD ["/usr/local/bin/blog-api"]
