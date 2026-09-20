BIN      := skm
PKG      := github.com/Bannercheck/SAP_Kernel_Manager
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -s -w -X $(PKG)/internal/version.Version=$(VERSION) -X $(PKG)/internal/version.Commit=$(COMMIT) -X $(PKG)/internal/version.Date=$(DATE)
export CGO_ENABLED := 0

# os/arch pairs that SAP kernels ship for and Go can target
TARGETS := linux/amd64 linux/ppc64le aix/ppc64 windows/amd64

.PHONY: build check test vet fmt cross clean

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BIN) ./cmd/skm

check: fmt vet test build

fmt:
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || (echo "gofmt: files need formatting" && exit 1)

vet:
	go vet ./...

test:
	go test ./...

# dist/ layout: skm.sh + skm.bat launchers, bin/skm-<os>-<arch> binaries.
# Nothing has to be installed on the SAP host: copy dist/ and run skm.sh / skm.bat.
cross:
	@mkdir -p dist/bin
	@for t in $(TARGETS); do \
	  os=$${t%/*}; arch=$${t#*/}; ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
	  out=dist/bin/$(BIN)-$$os-$$arch$$ext; echo "  $$out"; \
	  GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o $$out ./cmd/skm || exit 1; \
	done
	@cp scripts/skm.sh scripts/skm.bat dist/ && chmod +x dist/skm.sh dist/bin/*
	@cd dist && sha256sum bin/* skm.sh skm.bat > SHA256SUMS

clean:
	rm -rf bin dist
