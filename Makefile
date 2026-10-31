# dev entrypoints. go via ~/.local/go if not on PATH.
SHELL := /bin/bash
export PATH := $(HOME)/.local/go/bin:$(HOME)/go/bin:$(PATH)

DB_PORT ?= $(or $(CRAWLER_DB_PORT),5441)
DB_PASSWORD ?= $(or $(CRAWLER_DB_PASSWORD),crawler-dev-only)
TEST_DB_URL ?= postgres://crawler:$(DB_PASSWORD)@127.0.0.1:$(DB_PORT)/crawler?sslmode=disable
VERSION ?= $(shell git describe --always --dirty 2>/dev/null || echo dev)

.PHONY: help db-up db-down db-reset build run test test-short fmt fmt-check vet lint vuln check generate tidy up up-full down logs docker-build web-check infra-verify infra-preview

help:
	@grep -E '^[a-z-]+:' Makefile | cut -d: -f1 | sort

db-up:
	docker compose up -d --wait db

db-down:
	docker compose down

db-reset:
	docker compose down -v && docker compose up -d --wait db

build:
	go build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/crawler ./cmd/crawler

test: db-up
	CRAWLER_TEST_DATABASE_URL='$(TEST_DB_URL)' go test -race -count=1 ./...

test-short:
	go test -race -short ./...

fmt:
	gofmt -w $$(git ls-files '*.go' | grep -v '\.gen\.go$$')

fmt-check:
	@out=$$(gofmt -l $$(git ls-files '*.go')); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

lint:
	golangci-lint run ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

check: fmt-check vet lint vuln

generate:
	go generate ./...

tidy:
	go mod tidy

# full local stack (db + migrate + api).
up:
	CRAWLER_VERSION=$(VERSION) docker compose up -d --build --wait db migrate api worker

# full stack + web ui (http://localhost:3000).
up-full:
	CRAWLER_VERSION=$(VERSION) docker compose --profile full up -d --build --wait db migrate api worker web

down:
	docker compose --profile full down

logs:
	docker compose logs -f --tail=200 $(SVC)

docker-build:
	docker build -f deploy/Dockerfile --build-arg VERSION=$(VERSION) -t crawler-go:dev .

# web (admin spa): same checks as its ci job.
web-check:
	cd web && yarn install --frozen-lockfile && yarn api:check && yarn run check && yarn lint && yarn test && yarn build

# pulumi k3s deploy (docs/guides/deploy-k3s.md). node 26 + yarn 1.22.
INFRA_STACK ?= local
infra-verify:
	cd infra && yarn install --frozen-lockfile && yarn verify

infra-preview:
	cd infra && pulumi preview --stack $(INFRA_STACK)
