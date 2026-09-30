BIN     := bin/boku
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test race short lint sample site clean

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

# Assemble the GitHub Pages site into ./_site (site/ is self-contained; this
# refreshes the report images from docs/images).
site:
	rm -rf _site
	cp docs/images/report-page-*.png site/assets/img/
	cp -R site _site
	touch _site/.nojekyll
	@echo "site assembled in _site/ (preview: python3 -m http.server -d _site 8000)"

clean:
	rm -rf bin tmp _site
