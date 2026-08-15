DOCKER ?= /Applications/Docker.app/Contents/Resources/bin/docker

.PHONY: dev stop test test-backend test-frontend test-integration build-cli build-api

dev:
	PATH="/Applications/Docker.app/Contents/Resources/bin:$$PATH" $(DOCKER) compose up -d --build

stop:
	PATH="/Applications/Docker.app/Contents/Resources/bin:$$PATH" $(DOCKER) compose down

test: test-backend test-frontend test-integration

test-backend:
	cd backend && go test ./...

test-frontend:
	cd frontend && npm run typecheck && npm run build

test-integration:
	pytest

build-cli:
	mkdir -p bin
	cd backend && go build -o ../bin/termind ./cmd/termind

build-api:
	mkdir -p bin
	cd backend && go build -o ../bin/termind-api ./cmd/server

