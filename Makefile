.PHONY: build test test-race lint vet fmt tidy clean install

BINARY := koseven-lsp
MODULE  := github.com/akyrey/koseven-lsp

build:
	go build -o $(BINARY) ./cmd/koseven-lsp

test:
	go test ./... -count=1

test-race:
	go test -race ./... -count=1

lint:
	golangci-lint run ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

tidy:
	go mod tidy && go mod verify

clean:
	rm -f $(BINARY)

install:
	go install ./cmd/koseven-lsp
