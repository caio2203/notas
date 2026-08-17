BINARY  := notas
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -s -w -X main.version=$(VERSION)
PREFIX  ?= /usr/local

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/notas

install: build
	install -Dm755 bin/$(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/$(BINARY)

release:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -ldflags "$(LDFLAGS)" -trimpath -o bin/$(BINARY) ./cmd/notas

build-all:
	GOOS=linux  GOARCH=amd64  go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-amd64  ./cmd/notas
	GOOS=linux  GOARCH=arm64  go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-arm64  ./cmd/notas
	GOOS=darwin GOARCH=arm64  go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-darwin-arm64 ./cmd/notas

test:
	go test ./... -race -cover

lint:
	golangci-lint run ./...
