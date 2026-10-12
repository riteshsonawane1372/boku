BIN     := bin/boku
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test race short lint sample site serve clean

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

# Build the website (a Hugo site: hugo.toml, sources in site/) into ./_site.
# Screenshots and example files are mounted from docs/, not copied. Nothing is
# deployed here: the Pages workflow builds and publishes on every push to main.
site:
	hugo --gc --minify --cleanDestinationDir
	touch _site/.nojekyll
	@echo "site built in _site/"

# Preview the website with live reload at http://localhost:1313.
serve:
	hugo server

clean:
	rm -rf bin tmp _site resources .hugo_build.lock
