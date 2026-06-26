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

.DEFAULT_GOAL := help

.PHONY: help build install

help:
	@echo "usage: make <target> [BINDIR=$(BINDIR)]"
	@echo ""
	@echo "  Development:"
	@echo "    $(CYAN)build$(CLEAR) : Build the $(BINARY) binary at $(BUILD_OUTPUT)"
	@echo ""
	@echo "  Install:"
	@echo "    $(CYAN)install$(CLEAR) : Install $(BINARY) and $(ALIAS) alias to $(BINDIR)"

build:
	@install -d "$(BUILD_DIR)"
	@$(GO) build -o "$(BUILD_OUTPUT)" ./cmd/factile
	@printf '$(GREEN)built$(CLEAR) %s\n' "$(BUILD_OUTPUT)"

install: build
	@install -d "$(BINDIR)"
	@install -m 0755 "$(BUILD_OUTPUT)" "$(BINDIR)/$(BINARY)"
	@printf '$(GREEN)installed$(CLEAR) %s\n' "$(BINDIR)/$(BINARY)"
	@ln -sf "$(BINARY)" "$(BINDIR)/$(ALIAS)"
	@printf '$(GREEN)installed$(CLEAR) %s -> %s\n' "$(BINDIR)/$(ALIAS)" "$(BINARY)"
