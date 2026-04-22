# feivpn-runtime — agent-driven FeiVPN bootstrap CLI

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

BIN_DIR := dist
FEIVPNCTL := $(BIN_DIR)/feivpnctl-$(GOOS)-$(GOARCH)

.PHONY: all build build-all test lint clean tarball sync-bins verify-bins help

all: build

help:
	@echo "Targets:"
	@echo "  build       — compile feivpnctl for the current host"
	@echo "  build-all   — cross-compile feivpnctl for all 4 release targets"
	@echo "                (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64)"
	@echo "                via scripts/build-cli-binaries.sh"
	@echo "  test        — go test ./..."
	@echo "  lint        — go vet ./..."
	@echo "  tarball     — produce dist/feivpn-runtime-{os}-{arch}.tar.gz"
	@echo "  sync-bins   — fetch latest feivpn + feiapi binaries from"
	@echo "                feivpn/feivpn-apps Releases into bin/"
	@echo "  verify-bins — re-hash bin/ and check against manifest/binaries.manifest.json"
	@echo "  clean       — rm -rf dist/"

build:
	@mkdir -p $(BIN_DIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 \
	  go build -trimpath -ldflags "$(LDFLAGS)" \
	  -o $(FEIVPNCTL) ./cmd/feivpnctl
	@echo "Built $(FEIVPNCTL)"

build-all:
	./scripts/build-cli-binaries.sh --version $(VERSION)

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -rf $(BIN_DIR)

# Self-contained release tarball:
#   feivpnctl                        (just-built, current host)
#   bin/feivpn-{os}-{arch}           (pinned, from this repo)
#   bin/feiapi-{os}-{arch}           (pinned, from this repo)
#   schema/, templates/              (verbatim)
#   manifest/binaries.manifest.json    (so the installer can verify)
tarball: build
	@mkdir -p $(BIN_DIR)/pkg
	cp $(FEIVPNCTL) $(BIN_DIR)/pkg/feivpnctl
	cp -R bin       $(BIN_DIR)/pkg/bin
	cp -R schema    $(BIN_DIR)/pkg/schema
	cp -R templates $(BIN_DIR)/pkg/templates
	cp manifest/binaries.manifest.json $(BIN_DIR)/pkg/manifest.json
	cp LICENSE README.md SKILL.md $(BIN_DIR)/pkg/ 2>/dev/null || true
	tar -C $(BIN_DIR) -czf $(BIN_DIR)/feivpn-runtime-$(GOOS)-$(GOARCH).tar.gz pkg
	rm -rf $(BIN_DIR)/pkg
	shasum -a 256 $(BIN_DIR)/feivpn-runtime-$(GOOS)-$(GOARCH).tar.gz \
	  > $(BIN_DIR)/feivpn-runtime-$(GOOS)-$(GOARCH).tar.gz.sha256
	@echo "Tarball: $(BIN_DIR)/feivpn-runtime-$(GOOS)-$(GOARCH).tar.gz"

sync-bins:
	./scripts/sync-bins.sh

verify-bins:
	./scripts/verify-bins.sh
