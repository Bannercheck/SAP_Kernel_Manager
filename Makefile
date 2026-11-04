BIN      := kernelman
PKG      := github.com/Bannercheck/SAP_Kernel_Manager
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -s -w -X $(PKG)/internal/version.Version=$(VERSION) -X $(PKG)/internal/version.Commit=$(COMMIT) -X $(PKG)/internal/version.Date=$(DATE)
export CGO_ENABLED := 0
# transcripts are recorded with colours and glyphs forced on
EXENV := KERNELMAN_COLOR=always KERNELMAN_UNICODE=1

# os/arch pairs that SAP kernels ship for and Go can target
TARGETS := linux/amd64 linux/ppc64le aix/ppc64
# developer/demo targets (no SAP, `kernelman demo` only)
DEV_TARGETS := darwin/arm64 darwin/amd64

.PHONY: build check test vet fmt cross clean examples macos package hub hub-agents hub-dist hub-examples vuln

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BIN) ./cmd/kernelman

check: fmt vet test build

fmt:
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || (echo "gofmt: files need formatting" && exit 1)

vet:
	go vet ./...

test:
	go test ./...

# dist/ layout: kernelman.sh launcher, bin/kernelman-<os>-<arch> binaries.
# Nothing has to be installed on the SAP host: copy dist/ and run kernelman.sh / kernelman.bat.
cross:
	@mkdir -p dist/bin
	@for t in $(TARGETS) $(DEV_TARGETS); do \
	  os=$${t%/*}; arch=$${t#*/}; ext=""; \
	  out=dist/bin/$(BIN)-$$os-$$arch$$ext; echo "  $$out"; \
	  GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o $$out ./cmd/kernelman || exit 1; \
	done
	@cp internal/ship/kernelman.sh dist/ && chmod +x dist/kernelman.sh dist/bin/*
	@cd dist && sha256sum bin/* kernelman.sh > SHA256SUMS

# Regenerate docs/examples/*.txt transcripts and *.png screenshots.
# Transcripts come from fake-runner test scenarios (KERNELMAN_WRITE_EXAMPLE=1) and from
# running the real binary on this (non-SAP) host; PNGs need the bundled Chromium.
examples: build
	@rm -f docs/examples/*.txt docs/examples/*.png
	@KERNELMAN_WRITE_EXAMPLE=1 go test ./internal/cli >/dev/null
	@{ echo '$$ ./kernelman.sh version'; ./bin/kernelman version; } > docs/examples/version.txt
	@{ echo '$$ ./kernelman.sh status      # SAP kurulu olmayan bir hostta'; $(EXENV) ./bin/kernelman status; echo "exit code: $$?"; } > docs/examples/status-nosap.txt
	@rm -rf /tmp/kernelman-demo-example && { echo '$$ ./kernelman.sh demo         # simüle SAP hostu · 2 → Y → M · 5 → K → M · q'; printf '2\ny\nm\n5\nk\nm\nq\n' | KERNELMAN_DEMO=1 KERNELMAN_DEMO_ROOT=/tmp/kernelman-demo-example KERNELMAN_MENU=1 $(EXENV) ./bin/kernelman | sed 's|/tmp/kernelman-demo-example|~/.kernelman/demo|g'; } > docs/examples/demo.txt
	@{ echo '$$ make cross'; $(MAKE) -s cross 2>&1 | sed 's/^/  /'; echo; echo '$$ ls -la dist dist/bin'; ls -la dist dist/bin | sed 's/^/  /'; echo; echo '$$ file dist/bin/*'; file dist/bin/* | sed 's/,.*//;s/^/  /'; } > docs/examples/cross-build.txt
	@for f in docs/examples/*.txt; do python3 scripts/screen2png.py $$f $${f%.txt}.png "$(BIN) — $$(basename $${f%.txt})" >/dev/null || exit 1; done
	@ls docs/examples/*.png

# Server package for Linux/AIX: launcher + binaries + checksums, as tar.gz (AIX has tar/gzip, not always unzip)
package: cross
	@rm -rf dist/kernelman && mkdir -p dist/kernelman/bin
	@cp dist/bin/$(BIN)-linux-* dist/bin/$(BIN)-aix-* dist/kernelman/bin/ && cp internal/ship/kernelman.sh dist/kernelman/ && cp README.md dist/kernelman/
	@cd dist/kernelman && sha256sum bin/* kernelman.sh > SHA256SUMS
	@cd dist && rm -f kernelman-$(VERSION).tar.gz && tar czf kernelman-$(VERSION).tar.gz kernelman && ls -la kernelman-$(VERSION).tar.gz

# macOS demo package: launcher + darwin binaries + Turkish quick start
macos: cross
	@rm -rf dist/macos && mkdir -p dist/macos/bin
	@cp dist/bin/$(BIN)-darwin-* dist/macos/bin/ && cp internal/ship/kernelman.sh dist/macos/ && cp docs/MACOS-DEMO.md dist/macos/README.md
	@cd dist && rm -f kernelman-macos-demo.zip && zip -qr kernelman-macos-demo.zip macos && ls -la kernelman-macos-demo.zip

# ---------------------------------------------------------------- KernelMan Hub
# The hub embeds the agent builds for every SAP platform (internal/hub/agents/).
HUB_TARGETS := linux/amd64 linux/ppc64le windows/amd64 darwin/arm64
GOVULNCHECK := golang.org/x/vuln/cmd/govulncheck@v1.9.0

hub-agents:
	@for t in $(TARGETS); do \
	  os=$${t%/*}; arch=$${t#*/}; out=internal/hub/agents/$(BIN)-$$os-$$arch; echo "  agent $$out"; \
	  GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o $$out ./cmd/kernelman || exit 1; \
	done

hub: hub-agents
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/kernelman-hub ./cmd/kernelman-hub

# dist/hub/kernelman-hub-linux-amd64.tar.gz (RHEL/SLES: binary + systemd unit + quick start)
# dist/hub/kernelman-hub-windows-amd64.zip  (kernelman-hub.exe + quick start)
hub-dist: hub-agents
	@mkdir -p dist/hub
	@for t in $(HUB_TARGETS); do \
	  os=$${t%/*}; arch=$${t#*/}; ext=""; [ $$os = windows ] && ext=.exe; \
	  d=dist/hub/kernelman-hub-$$os-$$arch; mkdir -p $$d; echo "  $$d/kernelman-hub$$ext"; \
	  GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o $$d/kernelman-hub$$ext ./cmd/kernelman-hub || exit 1; \
	  cp docs/HUB.md $$d/README.md; \
	  if [ $$os = linux ]; then cp scripts/kernelman-hub.service $$d/; (cd dist/hub && tar czf kernelman-hub-$$os-$$arch.tar.gz kernelman-hub-$$os-$$arch); \
	  else (cd dist/hub && zip -qr kernelman-hub-$$os-$$arch.zip kernelman-hub-$$os-$$arch); fi; \
	done
	@cd dist/hub && sha256sum *.tar.gz *.zip > SHA256SUMS && ls -la *.tar.gz *.zip

# Screenshots of the dashboard (demo mode, headless Chromium through Playwright)
hub-examples:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/kernelman-hub ./cmd/kernelman-hub
	@./bin/kernelman-hub --demo --listen 127.0.0.1:18090 --no-browser >/dev/null 2>&1 & pid=$$!; sleep 4; \
	  NODE_PATH=$$(npm root -g) node scripts/hub-screens.cjs http://127.0.0.1:18090 docs/examples; rc=$$?; kill $$pid; exit $$rc

# Known vulnerabilities in the code and its dependencies (Go vulnerability database)
vuln:
	GOTOOLCHAIN=$$(go env GOVERSION) go run $(GOVULNCHECK) ./...

clean:
	rm -rf bin dist
