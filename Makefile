# 常用开发命令（MVP）

.PHONY: build test web-install web-build web-test run fmt

build:
	go build ./...

test:
	go test ./...

web-install:
	cd web && pnpm install --frozen-lockfile

web-build:
	cd web && pnpm build

web-test:
	cd web && pnpm test -- --run

run:
	go run ./cmd/server

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './web/*')
