FROM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/blog-api ./cmd/api

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin blog \
    && mkdir /data && chown blog:blog /data && chmod 700 /data
COPY --from=build /out/blog-api /usr/local/bin/blog-api
USER blog
ENV BLOG_ADDR=0.0.0.0:8080 BLOG_DB_PATH=/data/blog.db
EXPOSE 8080
CMD ["/usr/local/bin/blog-api"]
