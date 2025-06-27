# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec
BUILD_IMAGES ?= stackit-csi-plugin
SOURCES := Makefile go.mod go.sum $(shell find $(DEST) -name '*.go' 2>/dev/null)
VERSION ?= $(shell git describe --dirty --tags --match='v*')
REGISTRY ?= reg3.infra.ske.eu01.stackit.cloud/stackitcloud/cloud-provider-stackit
PLATFORMS ?= amd64 arm64
LDFLAGS := "-w -s -X 'github.com/stackitcloud/cloud-provider-stackit/pkg/util/version.Version=$(VERSION)'"

.PHONY: all
all: verify

##@ Tools

include ./hack/tools.mk

build: $(BUILD_IMAGES)

$(BUILD_IMAGES): $(SOURCES)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) GOPROXY=${GOPROXY} go build \
		-trimpath \
		-ldflags $(LDFLAGS) \
		-o $@ \
		cmd/$@/main.go

# Build a single image for the local default platform and push to the local
# container engine
build-local-image-%:
	docker buildx build --output type=docker \
		--build-arg VERSION=$(VERSION) \
		--tag $(REGISTRY)/$*:$(VERSION) \
		--platform $(shell echo $(addprefix linux/,$(PLATFORMS)) | sed 's/ /,/g') \
		--target $* \
		.

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
MOCK_SERVICES := iaas loadbalancer

.PHONY: mocks
mocks: $(MOCKGEN)
	@for service in $(MOCK_SERVICES); do \
		INTERFACES=`go doc -all github.com/stackitcloud/stackit-sdk-go/services/$$service | grep '^type Api.* interface' | sed -n 's/^type \(.*\) interface.*/\1/p' | paste -sd,`,DefaultApi; \
		$(MOCKGEN) -destination ./pkg/mock/$$service/$$service.go -package $$service github.com/stackitcloud/stackit-sdk-go/services/$$service $$INTERFACES; \
	done
	@$(MOCKGEN) -destination ./pkg/stackit/iaas_mock.go -package stackit ./pkg/stackit IaasClient
	@$(MOCKGEN) -destination ./pkg/stackit/loadbalancer_mock.go -package stackit ./pkg/stackit LoadbalancerClient
	@$(MOCKGEN) -destination ./pkg/util/mount/mount_mock.go -package mount ./pkg/util/mount IMount
	@$(MOCKGEN) -destination ./pkg/util/metadata/metadata_mock.go -package metadata ./pkg/util/metadata IMetadata

.PHONY: generate
generate: mocks
	go generate ./...
