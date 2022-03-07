# Copyright 2022 Dhi Aurrahman
# Licensed under the Apache License, Version 2.0 (the "License")

include Tools.mk

name := proxy

# VERSION is used in release artifacts names. This should be in <major>.<minor>.<patch> (without v
# prefix). When generating actual release assets this can be resolved using "git describe --tags --long".
VERSION ?= dev

# Root dir returns absolute path of current directory. It has a trailing "/".
root_dir := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

# Currently we resolve it using which. But more sophisticated approach is to use infer GOROOT.
go     := $(shell which go)
goarch := $(shell $(go) env GOARCH)
goexe  := $(shell $(go) env GOEXE)
goos   := $(shell $(go) env GOOS)

# Local cache directory.
CACHE_DIR ?= $(root_dir).cache

e2e_go_sources        := $(wildcard e2e/*.go e2e/*/*.go)
all_nongen_go_sources := $(wildcard *.go e2e/*.go handler/*.go handler/*/*.go internal/*/*.go runner/*.go xds-server/*.go)
all_go_sources        := $(all_nongen_go_sources)
main_go_sources       := $(wildcard $(filter-out %_test.go $(e2e_go_sources),$(all_go_sources)))
testable_go_packages  := $(sort $(foreach f,$(dir $(main_go_sources)),$(if $(findstring ./,$(f)),./,./$(f))))

current_binary_path := build/proxy_$(goos)_$(goarch)
current_binary      := $(current_binary_path)/proxy$(goexe)

linux_platforms       := linux_amd64 linux_arm64
non_windows_platforms := darwin_amd64 darwin_arm64 $(linux_platforms)
windows_platforms     := windows_amd64

archives := $(non_windows_platforms:%=dist/$(name)_$(VERSION)_%.tar.gz) $(windows_platforms:%=dist/$(name)_$(VERSION)_%.zip)

# Go tools directory holds the binaries of Go-based tools.
go_tools_dir := $(CACHE_DIR)/tools/go

export PATH := $(go_tools_dir):$(PATH)

# Go-based tools.
addlicense    := $(go_tools_dir)/addlicense
goimports     := $(go_tools_dir)/goimports
golangci-lint := $(go_tools_dir)/golangci-lint

# This is adopted from https://github.com/tetratelabs/func-e/blob/3df66c9593e827d67b330b7355d577f91cdcb722/Makefile#L60-L76.
# ANSI escape codes. f_ means foreground, b_ background.
# See https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_(Select_Graphic_Rendition)_parameters.
f_black            := $(shell printf "\33[30m")
b_black            := $(shell printf "\33[40m")
f_white            := $(shell printf "\33[97m")
f_gray             := $(shell printf "\33[37m")
f_dark_gray        := $(shell printf "\33[90m")
f_bright_cyan      := $(shell printf "\33[96m")
b_bright_cyan      := $(shell printf "\33[106m")
ansi_reset         := $(shell printf "\33[0m")
ansi_$(name)       := $(b_black)$(f_black)$(b_bright_cyan)$(name)$(ansi_reset)
ansi_format_dark   := $(f_gray)$(f_bright_cyan)%-10s$(ansi_reset) $(f_dark_gray)%s$(ansi_reset)\n
ansi_format_bright := $(f_white)$(f_bright_cyan)%-10s$(ansi_reset) $(f_black)$(b_bright_cyan)%s$(ansi_reset)\n

# This formats help statements in ANSI colors. To hide a target from help, don't comment it with a trailing '##'.
help: ## Describe how to use each target
	@printf "$(ansi_$(name))$(f_white)\n"
	@awk 'BEGIN {FS = ":.*?## "} /^[0-9a-zA-Z_-]+:.*?## / {sub("\\\\n",sprintf("\n%22c"," "), $$2);printf "$(ansi_format_dark)", $$1, $$2}' $(MAKEFILE_LIST)

build: $(current_binary) ## Build the proxy binary

# This generates the assets that attach to a release.
# TODO(dio): Generate checksums for the assets.
dist: $(archives) ## Generate release assets

format: go.mod $(all_nongen_go_sources) $(goimports) ## Format all Go sources
	@printf "$(ansi_format_dark)" $@ "formatting files"
	@$(go) mod tidy
	@$(go)fmt -s -w $(all_nongen_go_sources)
# Workaround inconsistent goimports grouping with awk until golang/go#20818 or incu6us/goimports-reviser#50
	@for f in $(all_nongen_go_sources); do \
			awk '/^import \($$/,/^\)$$/{if($$0=="")next}{print}' $$f > /tmp/fmt; \
	    mv /tmp/fmt $$f; \
	done
	@$(goimports) -local $$(sed -ne 's/^module //gp' go.mod) -w $(all_nongen_go_sources)
	@printf "$(ansi_format_bright)" $@ "ok"

# Override lint cache directory. https://golangci-lint.run/usage/configuration/#cache.
export GOLANGCI_LINT_CACHE=$(CACHE_DIR)/golangci-lint
lint: .golangci.yml $(all_nongen_go_sources) $(golangci-lint) ## Lint all Go sources
	@printf "$(ansi_format_dark)" $@ "linting files"
	@$(golangci-lint) run --timeout 5m --config $< ./...
	@printf "$(ansi_format_bright)" $@ "ok"

clean: ## Ensure a clean build
	@printf "$(ansi_format_dark)" $@ "deleting temporary files"
	@rm -rf coverage.txt
	@rm -rf build
	@rm -rf dist
	@$(go) clean -testcache
	@printf "$(ansi_format_bright)" $@ "ok"

build/$(name)_%/$(name): $(main_go_sources)
	$(call go-build,$@,$<)

build/$(name)_%/$(name).exe: $(main_go_sources)
	$(call go-build,$@,$<)

dist/$(name)_$(VERSION)_%.tar.gz: build/$(name)_%/$(name)
	@printf "$(ansi_format_dark)" tar.gz "tarring $@"
	@mkdir -p $(@D)
	@tar -C $(<D) -cpzf $@ $(<F)
	@printf "$(ansi_format_bright)" tar.gz "ok"

# TODO(dio): Archive the signed binary instead of the unsigned one. And provide pivot when
# building on Windows.
dist/$(name)_$(VERSION)_%.zip: build/$(name)_%/$(name).exe
	@printf "$(ansi_format_dark)" zip "zipping $@"
	@mkdir -p $(@D)
	@zip -qj $@ $<
	@printf "$(ansi_format_bright)" zip "ok"

go_link := -X main.version=$(VERSION) -X main.commit=$(shell git rev-parse --short HEAD)
go-arch  = $(if $(findstring amd64,$1),amd64,arm64)
go-os    = $(if $(findstring .exe,$1),windows,$(if $(findstring linux,$1),linux,darwin))
define go-build
	@printf "$(ansi_format_dark)" build "building $1"
	@CGO_ENABLED=0 GOOS=$(call go-os,$1) GOARCH=$(call go-arch,$1) $(go) build \
		-ldflags "-s -w $(go_link)" \
		-o $1 $2
	@printf "$(ansi_format_bright)" build "ok"
endef

# Catch all rules for Go-based tools.
$(go_tools_dir)/%:
	@printf "$(ansi_format_dark)" tools "installing $($(notdir $@)@v)..."
	@GOBIN=$(go_tools_dir) go install $($(notdir $@)@v)
	@printf "$(ansi_format_bright)" tools "ok"
