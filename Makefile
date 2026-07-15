CLEAR=\033[0m
GREEN=\033[0;32m
CYAN=\033[0;36m

# Configuration
GO ?= go
GOCACHE ?= /tmp/factile-go-build-cache
GOMODCACHE ?= /tmp/factile-go-mod-cache
BINARY ?= factile
ALIAS ?= ft
BUILD_DIR ?= bin
BUILD_OUTPUT ?= $(BUILD_DIR)/$(BINARY)
LOCAL_BINDIR := $(HOME)/.local/bin
BINDIR ?= $(if $(findstring :$(LOCAL_BINDIR):,:$(PATH):),$(LOCAL_BINDIR),/usr/local/bin)
FACTILE_UI_DIR ?= ../factile-ui
FACTILE_UI_DIST ?= $(FACTILE_UI_DIR)/apps/local/dist
VERSION_FILE := VERSION

export GOCACHE
export GOMODCACHE

.DEFAULT_GOAL := help

.PHONY: help build ui-assets smoke-ui install verify pre-release version release-fix release-feature release-major

help:
	@echo "usage: make <target> [BINDIR=$(BINDIR)]"
	@echo ""
	@echo "  Development:"
	@echo "    $(CYAN)build$(CLEAR) : Build the $(BINARY) binary at $(BUILD_OUTPUT)"
	@echo "    $(CYAN)ui-assets$(CLEAR) : Build factile-ui and refresh embedded UI assets"
	@echo "    $(CYAN)smoke-ui$(CLEAR) : Build and smoke-test the embedded UI bridge"
	@echo "    $(CYAN)verify$(CLEAR) : Run the public Go test suite"
	@echo "    $(CYAN)pre-release$(CLEAR) : Run the complete local release gate"
	@echo ""
	@echo "  Release:"
	@echo "    $(CYAN)version$(CLEAR) : Print current version (X.Y.Z)"
	@echo "    $(CYAN)release-fix$(CLEAR) : Bump patch version number and release"
	@echo "    $(CYAN)release-feature$(CLEAR) : Bump minor version number and release"
	@echo "    $(CYAN)release-major$(CLEAR) : Bump major version number and release"
	@echo ""
	@echo "  Install:"
	@echo "    $(CYAN)install$(CLEAR) : Install $(BINARY) and $(ALIAS) alias to $(BINDIR)"

build:
	@install -d "$(BUILD_DIR)"
	@$(GO) build -o "$(BUILD_OUTPUT)" ./cmd/factile
	@printf '$(GREEN)built$(CLEAR) %s\n' "$(BUILD_OUTPUT)"

ui-assets:
	@cd "$(FACTILE_UI_DIR)" && npm run build
	@FACTILE_UI_DIST="$(FACTILE_UI_DIST)" ./scripts/sync-ui-assets.sh

smoke-ui:
	@GO="$(GO)" BINARY="$(BUILD_OUTPUT)" ./scripts/smoke-ui.sh

verify:
	@$(GO) test ./...

pre-release:
	@./scripts/verify.sh
	@$(MAKE) smoke-ui

version:
	@cat "$(VERSION_FILE)"

release-fix:
	@./scripts/release patch

release-feature:
	@./scripts/release minor

release-major:
	@./scripts/release major

install: build
	@install -d "$(BINDIR)"
	@install -m 0755 "$(BUILD_OUTPUT)" "$(BINDIR)/$(BINARY)"
	@printf '$(GREEN)installed$(CLEAR) %s\n' "$(BINDIR)/$(BINARY)"
	@ln -sf "$(BINARY)" "$(BINDIR)/$(ALIAS)"
	@printf '$(GREEN)installed$(CLEAR) %s -> %s\n' "$(BINDIR)/$(ALIAS)" "$(BINARY)"
