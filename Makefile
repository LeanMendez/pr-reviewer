.PHONY: build test lint

build:
	go build -o bin/pr-review ./cmd/pr-review

test:
	go test ./...

lint:
	go vet ./... && test -z "$$(gofmt -l .)"
