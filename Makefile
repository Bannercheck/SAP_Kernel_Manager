BIN      := sapkernel
PKG      := github.com/Bannercheck/SAP_Kernel_Manager
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -s -w -X $(PKG)/internal/version.Version=$(VERSION) -X $(PKG)/internal/version.Commit=$(COMMIT) -X $(PKG)/internal/version.Date=$(DATE)
export CGO_ENABLED := 0
# transcripts are recorded with colours and glyphs forced on
EXENV := SAPKERNEL_COLOR=always SAPKERNEL_UNICODE=1

# os/arch pairs that SAP kernels ship for and Go can target
TARGETS := linux/amd64 linux/ppc64le aix/ppc64 windows/amd64

.PHONY: build check test vet fmt cross clean examples

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BIN) ./cmd/sapkernel

check: fmt vet test build

fmt:
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || (echo "gofmt: files need formatting" && exit 1)

vet:
	go vet ./...

test:
	go test ./...

# dist/ layout: sapkernel.sh + sapkernel.bat launchers, bin/sapkernel-<os>-<arch> binaries.
# Nothing has to be installed on the SAP host: copy dist/ and run sapkernel.sh / sapkernel.bat.
cross:
	@mkdir -p dist/bin
	@for t in $(TARGETS); do \
	  os=$${t%/*}; arch=$${t#*/}; ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
	  out=dist/bin/$(BIN)-$$os-$$arch$$ext; echo "  $$out"; \
	  GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o $$out ./cmd/sapkernel || exit 1; \
	done
	@cp scripts/sapkernel.sh scripts/sapkernel.bat dist/ && chmod +x dist/sapkernel.sh dist/bin/*
	@cd dist && sha256sum bin/* sapkernel.sh sapkernel.bat > SHA256SUMS

# Regenerate docs/examples/*.txt transcripts and *.png screenshots.
# Transcripts come from fake-runner test scenarios (SAPKERNEL_WRITE_EXAMPLE=1) and from
# running the real binary on this (non-SAP) host; PNGs need the bundled Chromium.
examples: build
	@rm -f docs/examples/*.txt docs/examples/*.png
	@SAPKERNEL_WRITE_EXAMPLE=1 go test ./internal/cli >/dev/null
	@{ echo '$$ ./sapkernel.sh version'; ./bin/sapkernel version; } > docs/examples/version.txt
	@{ echo '$$ ./sapkernel.sh status      # SAP kurulu olmayan bir hostta'; $(EXENV) ./bin/sapkernel status; echo "exit code: $$?"; } > docs/examples/status-nosap.txt
	@{ echo '$$ ./sapkernel.sh              # argümansız: menü (10 = Version, 1 = SAP Status)'; printf '10\n\n1\n\nq\n' | SAPKERNEL_MENU=1 $(EXENV) ./bin/sapkernel; } > docs/examples/menu.txt
	@{ echo '$$ make cross'; $(MAKE) -s cross 2>&1 | sed 's/^/  /'; echo; echo '$$ ls -la dist dist/bin'; ls -la dist dist/bin | sed 's/^/  /'; echo; echo '$$ file dist/bin/*'; file dist/bin/* | sed 's/,.*//;s/^/  /'; } > docs/examples/cross-build.txt
	@for f in docs/examples/*.txt; do python3 scripts/screen2png.py $$f $${f%.txt}.png "$(BIN) — $$(basename $${f%.txt})" >/dev/null || exit 1; done
	@ls docs/examples/*.png

clean:
	rm -rf bin dist
