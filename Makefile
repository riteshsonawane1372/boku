BIN     := bin/boku
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test race short lint sample clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/boku

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/boku

test:
	go test ./...

race:
	go test -race ./...

short:
	go test -short ./...

lint:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)
	go vet ./...

# Render the fictional sample report (testdata/sample) to ./tmp.
sample:
	mkdir -p tmp
	BOKU_RENDER_OUT=$(CURDIR)/tmp go test ./internal/render -run 'TestPDF|TestHTMLRendersAllParts' -count=1
	@echo "wrote tmp/sample.pdf, tmp/sample.html, tmp/sample.md"

clean:
	rm -rf bin tmp
