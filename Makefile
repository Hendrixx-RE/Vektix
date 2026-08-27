.PHONY: all build clean test install

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

all: build

build:
	@mkdir -p bin
	go build $(LDFLAGS) -o bin/vektix ./cmd/vektix

install:
	go install $(LDFLAGS) ./cmd/vektix

clean:
	rm -rf bin/

test:
	go test ./... -race -count=1
