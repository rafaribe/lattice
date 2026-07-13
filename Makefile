.PHONY: all build server agent cli clean test docker-server docker-agent lint

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

all: build

build: server agent cli

server:
	go build $(LDFLAGS) -o bin/lattice-server ./cmd/server

agent:
	go build $(LDFLAGS) -o bin/lattice-agent ./cmd/agent

cli:
	go build $(LDFLAGS) -o bin/lattice ./cmd/lattice

clean:
	rm -rf bin/

test:
	go test ./... -v -race

lint:
	golangci-lint run ./...

docker-server:
	docker build -t lattice-server:$(VERSION) -f deploy/Dockerfile.server .

docker-agent:
	docker build -t lattice-agent:$(VERSION) -f deploy/Dockerfile.agent .

docker: docker-server docker-agent

run-server:
	go run ./cmd/server --port 8090

run-agent:
	go run ./cmd/agent --server http://localhost:8090 --ollama http://localhost:11434
