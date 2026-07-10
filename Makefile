CLEAR=\033[0m
GREEN=\033[0;32m
CYAN=\033[0;36m

# Configuration
GO ?= go
BINARY ?= factile
ALIAS ?= ft
BUILD_DIR ?= bin
BUILD_OUTPUT ?= $(BUILD_DIR)/$(BINARY)
LOCAL_BINDIR := $(HOME)/.local/bin
BINDIR ?= $(if $(findstring :$(LOCAL_BINDIR):,:$(PATH):),$(LOCAL_BINDIR),/usr/local/bin)
FACTILE_UI_DIR ?= ../factile-ui
FACTILE_UI_DIST ?= $(FACTILE_UI_DIR)/apps/local/dist

.DEFAULT_GOAL := help

.PHONY: help build ui-assets smoke-ui install verify conformance-reader conformance-cli conformance-mcp conformance-okf conformance-root-layout conformance-writer conformance-writer-cli conformance-writer-mcp

help:
	@echo "usage: make <target> [BINDIR=$(BINDIR)]"
	@echo ""
	@echo "  Development:"
	@echo "    $(CYAN)build$(CLEAR) : Build the $(BINARY) binary at $(BUILD_OUTPUT)"
	@echo "    $(CYAN)ui-assets$(CLEAR) : Build factile-ui and refresh embedded UI assets"
	@echo "    $(CYAN)smoke-ui$(CLEAR) : Build and smoke-test the embedded UI bridge"
	@echo "    $(CYAN)verify$(CLEAR) : Run Go tests and checked-in conformance tests"
	@echo "    $(CYAN)conformance-reader$(CLEAR) : Run Reader Contract CLI and MCP conformance"
	@echo "    $(CYAN)conformance-cli$(CLEAR) : Run Reader Contract CLI JSON conformance"
	@echo "    $(CYAN)conformance-mcp$(CLEAR) : Run Reader Contract local MCP conformance"
	@echo "    $(CYAN)conformance-okf$(CLEAR) : Run OKF Bundle Contract parser and validation conformance"
	@echo "    $(CYAN)conformance-root-layout$(CLEAR) : Run Root and Source Layout Contract conformance"
	@echo "    $(CYAN)conformance-writer$(CLEAR) : Run Writer and Curator Contract CLI and MCP conformance"
	@echo "    $(CYAN)conformance-writer-cli$(CLEAR) : Run Writer and Curator Contract CLI JSON conformance"
	@echo "    $(CYAN)conformance-writer-mcp$(CLEAR) : Run Writer and Curator Contract local MCP conformance"
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

install: build
	@install -d "$(BINDIR)"
	@install -m 0755 "$(BUILD_OUTPUT)" "$(BINDIR)/$(BINARY)"
	@printf '$(GREEN)installed$(CLEAR) %s\n' "$(BINDIR)/$(BINARY)"
	@ln -sf "$(BINARY)" "$(BINDIR)/$(ALIAS)"
	@printf '$(GREEN)installed$(CLEAR) %s -> %s\n' "$(BINDIR)/$(ALIAS)" "$(BINARY)"

conformance-cli:
	@$(GO) test ./test/conformance -run TestReaderCLIJSONConformance -count=1

conformance-mcp:
	@$(GO) test ./test/conformance -run TestReaderMCPConformance -count=1

conformance-reader: conformance-cli conformance-mcp

conformance-okf:
	@$(GO) test ./test/conformance -run TestOKFCLIConformance -count=1

conformance-root-layout:
	@$(GO) test ./test/conformance -run TestRootLayout -count=1

conformance-writer-cli:
	@$(GO) test ./test/conformance -run TestWriterCLIJSONConformance -count=1

conformance-writer-mcp:
	@$(GO) test ./test/conformance -run TestWriterMCPConformance -count=1

conformance-writer: conformance-writer-cli conformance-writer-mcp
