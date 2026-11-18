.PHONY: lint test

lint:
	golangci-lint fmt --diff
	golangci-lint run --timeout 10m

test:
	go test -race ./...
	go build ./examples/...
