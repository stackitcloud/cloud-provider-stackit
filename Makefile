# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: verify

##@ Tools

include hack/tools.mk

.PHONY: clean-tools-bin
clean-tools-bin: ## Empty the tools binary directory.
	rm -rf $(TOOLS_BIN_DIR)/* $(TOOLS_BIN_DIR)/.version_*

.PHONY: fmt
fmt: $(GOIMPORTS_REVISER) ## Run go fmt against code.
	go fmt ./...
	$(GOIMPORTS_REVISER) .

.PHONY: modules
modules: ## Runs go mod to ensure modules are up to date.
	go mod tidy

.PHONY: test
test: ## Run tests.
	./hack/test.sh ./cmd/... ./pkg/...

.PHONY: test-cover
test-cover: ## Run tests with coverage.
	go test -coverprofile cover.out ./...
	go tool cover -html cover.out -o cover.html

##@ Verification

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run golangci-lint against code.
	$(GOLANGCI_LINT) run ./...

.PHONY: check
check: lint test ## Check everything (lint + test).

.PHONY: verify-fmt
verify-fmt: fmt ## Verify go code is formatted.
	@if !(git diff --quiet HEAD); then \
		echo "unformatted files detected, please run 'make fmt'"; exit 1; \
	fi

.PHONY: verify-modules
verify-modules: modules ## Verify go module files are up to date.
	@if !(git diff --quiet HEAD -- go.sum go.mod); then \
		echo "go module files are out of date, please run 'make modules'"; exit 1; \
	fi

.PHONY: verify
verify: verify-fmt verify-modules check

# generate mock types for the following services (space-separated list)
MOCK_SERVICES := iaas

.PHONY: mocks
mocks: $(MOCKGEN)
	@for service in $(MOCK_SERVICES); do \
		INTERFACES=`go doc -all github.com/stackitcloud/stackit-sdk-go/services/$$service | grep '^type Api.* interface' | sed -n 's/^type \(.*\) interface.*/\1/p' | paste -sd,`,DefaultApi; \
		$(MOCKGEN) -destination ./pkg/mock/$$service/$$service.go -package $$service github.com/stackitcloud/stackit-sdk-go/services/$$service $$INTERFACES; \
	done



.PHONY: generate
generate: mocks
	go generate ./...
