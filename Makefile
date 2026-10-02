BIN := bin/tunz

.PHONY: build test lint format generate mocks run clean

build:
	go build -o $(BIN) ./cmd/tunz

test:
	go test -race ./...

lint:
	golangci-lint run

format:
	golangci-lint fmt

generate:
	go generate ./...

mocks: generate

run: build
	$(BIN)

clean:
	rm -rf bin
