# dev entrypoints. go via ~/.local/go if not on PATH.
SHELL := /bin/bash
export PATH := $(HOME)/.local/go/bin:$(HOME)/go/bin:$(PATH)

DB_PORT ?= $(or $(CRAWLER_DB_PORT),5441)
DB_PASSWORD ?= $(or $(CRAWLER_DB_PASSWORD),crawler-dev-only)
TEST_DB_URL ?= postgres://crawler:$(DB_PASSWORD)@127.0.0.1:$(DB_PORT)/crawler?sslmode=disable
VERSION ?= $(shell git describe --always --dirty 2>/dev/null || echo dev)

.PHONY: help db-up db-down db-reset build run test test-short fmt fmt-check vet lint vuln check generate tidy up down logs docker-build

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

down:
	docker compose down

logs:
	docker compose logs -f --tail=200 $(SVC)

docker-build:
	docker build -f deploy/Dockerfile --build-arg VERSION=$(VERSION) -t crawler-go:dev .
