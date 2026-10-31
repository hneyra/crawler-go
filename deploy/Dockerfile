# syntax=docker/dockerfile:1
# crawler backend image. one binary, all subcommands.
# build: docker build -f deploy/Dockerfile --build-arg VERSION=$(git describe --always --dirty) -t crawler-go:dev .
# targets: (default) distroless runtime for api/migrate; `worker` = same binary + chromium
#   docker build -f deploy/Dockerfile --target worker -t crawler-go-worker:dev .

ARG GO_IMAGE=golang:1.27-trixie
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian13:nonroot
ARG WORKER_BASE_IMAGE=debian:trixie-slim

FROM ${GO_IMAGE} AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-mod=readonly
# deps first. layer cached until go.mod/go.sum change.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/crawler ./cmd/crawler

# worker: headless chromium for blocked/js sites (fetch fallback, SCROLL_AJAX, AJAX extraction).
# distro chromium: security updates via base image rebuilds. uid 65532 = distroless nonroot.
FROM ${WORKER_BASE_IMAGE} AS worker
ARG VERSION=dev
LABEL org.opencontainers.image.title="crawler-go-worker" \
      org.opencontainers.image.source="https://github.com/hneyra/crawler-go" \
      org.opencontainers.image.version="${VERSION}"
RUN apt-get update \
 && apt-get install -y --no-install-recommends chromium ca-certificates fonts-liberation fonts-noto-color-emoji \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd -g 65532 nonroot && useradd -u 65532 -g 65532 -M -d /data -s /usr/sbin/nologin nonroot \
 && mkdir -p /data && chown 65532:65532 /data
COPY --from=build /out/crawler /crawler
ENV CRAWLER_CHROME_PATH=/usr/bin/chromium \
    CRAWLER_RAW_DATA_DIR=/data/raw \
    CRAWLER_BROWSER_PROFILE_DIR=/data/profile
WORKDIR /data
USER 65532:65532
HEALTHCHECK --interval=15s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/crawler", "healthcheck", "http://127.0.0.1:8081/healthz"]
ENTRYPOINT ["/crawler"]
CMD ["worker"]

FROM ${RUNTIME_IMAGE}
ARG VERSION=dev
LABEL org.opencontainers.image.title="crawler-go" \
      org.opencontainers.image.source="https://github.com/hneyra/crawler-go" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /out/crawler /crawler
WORKDIR /
USER nonroot:nonroot
EXPOSE 8080 9091
# distroless: no shell, no curl. binary probes itself, loopback only.
HEALTHCHECK --interval=10s --timeout=4s --start-period=10s --retries=3 \
    CMD ["/crawler", "healthcheck"]
ENTRYPOINT ["/crawler"]
CMD ["serve"]
