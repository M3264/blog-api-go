FROM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/blog-api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate-json ./cmd/migrate-json

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin blog
COPY --from=build /out/blog-api /usr/local/bin/blog-api
COPY --from=build /out/migrate-json /usr/local/bin/migrate-json
USER blog
ENV BLOG_ADDR=0.0.0.0:8080
EXPOSE 8080
CMD ["/usr/local/bin/blog-api"]
