# syntax=docker/dockerfile:1
# crawler backend image. one binary, all subcommands.
# build: docker build -f deploy/Dockerfile --build-arg VERSION=$(git describe --always --dirty) -t crawler-go:dev .

ARG GO_IMAGE=golang:1.27-trixie
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian13:nonroot

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
