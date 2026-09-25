.DEFAULT_GOAL := dev

APP_REPLICAS ?= 3
export APP_REPLICAS

URL_COUNT ?= 100
REQUESTS ?= 10000
VUS ?= 50
MAX_DURATION ?= 2m
REDIRECT_P95_MS ?= 500
export URL_COUNT REQUESTS VUS MAX_DURATION REDIRECT_P95_MS

COMPOSE = docker compose -f dev/docker-compose.yml
COMPOSE_DEV = $(COMPOSE) -f dev/docker-compose.dev.yml

.PHONY: dev up down logs migrate vet test load-test

dev:
	$(COMPOSE_DEV) up --build -d

up:
	$(COMPOSE) up --build -d --wait

down:
	$(COMPOSE_DEV) down

logs:
	$(COMPOSE_DEV) logs -f

migrate:
	@go run ./cmd/migrate

vet:
	@go vet ./...

test:
	@go test -race ./...

load-test:
	bun run loadtests/short-url.ts
