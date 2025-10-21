# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec
BUILD_IMAGES ?= stackit-csi-plugin cloud-controller-manager
SOURCES := Makefile go.mod go.sum $(shell find $(DEST) -name '*.go' 2>/dev/null)
VERSION ?= $(shell git describe --dirty --tags --match='v*' 2>/dev/null || git rev-parse --short HEAD)
REGISTRY ?= ghcr.io
REPO ?= stackitcloud/cloud-provider-stackit
PLATFORMS ?= amd64 arm64
IS_DEV ?= true

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

.PHONY: images
images: $(foreach image,$(BUILD_IMAGES),image-$(image))

# lazy reference, evaluated when called
LOCAL = false
ifeq ($(LOCAL),true)
# includes busybox as extra dependency true check needed tools
APKO_EXTRA_PACKAGES = busybox
endif

image-%: $(APKO) $(KO)
	APKO_EXTRA_PACKAGES=$(APKO_EXTRA_PACKAGES) \
	LOCAL=$(LOCAL) \
	VERSION=$(VERSION) \
	PLATFORMS="$(PLATFORMS)" \
	REGISTRY=$(REGISTRY) \
	REPO=$(REPO)/$* \
	IS_DEV=$(IS_DEV) \
	./hack/build.sh $*

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

.PHONY: verify-generate
verify-generate: generate ## Verify go module files are up to date.
	@if !(git diff --quiet HEAD); then \
		echo "generate created a diff, please run 'make generate'"; exit 1; \
	fi

.PHONY: verify
verify: verify-fmt verify-modules verify-generate check

verify-e2e: verify-e2e-csi

verify-e2e-csi-sequential: FOCUS = "External.Storage.*(\[Feature:|\[Disruptive\]|\[Serial\])"
verify-e2e-csi-sequential: E2E_FLAGS = -v
verify-e2e-csi-sequential: verify-e2e-csi

verify-e2e-csi-parallel: FOCUS = "External.Storage"
verify-e2e-csi-parallel: SKIP = "\[Feature:|\[Disruptive\]|\[Serial\]"
verify-e2e-csi-parallel: E2E_FLAGS = -v -p
verify-e2e-csi-parallel: verify-e2e-csi

verify-e2e-csi: $(KUBERNETES_TEST)
	$(KUBERNETES_TEST_GINKGO) \
  	$(E2E_FLAGS) \
  	-focus=$(FOCUS) \
	-skip=$(SKIP) \
  	$(KUBERNETES_TEST) -- \
    -storage.testdriver=$(PWD)/test/e2e/csi/block-storage.yaml

verify-image-stackit-csi-plugin: LOCAL = true
verify-image-stackit-csi-plugin: APKO_EXTRA_PACKAGES = busybox
verify-image-stackit-csi-plugin: image-stackit-csi-plugin
	@echo "verifying binaries in image"
	@docker run -v ./tools/csi-deps-check.sh:/tools/csi-deps-check.sh --entrypoint=/tools/csi-deps-check.sh $(REGISTRY)/stackit-csi-plugin:$(VERSION) 

# generate mock types for the following services (space-separated list)
MOCK_SERVICES := iaas loadbalancer

.PHONY: mocks
mocks: $(MOCKGEN)
	# clean mocks
	@rm **/*/*_mock.go || true
	# generate mocks
	@for service in $(MOCK_SERVICES); do \
		INTERFACES=`go doc -all github.com/stackitcloud/stackit-sdk-go/services/$$service | grep '^type Api.* interface' | sed -n 's/^type \(.*\) interface.*/\1/p' | paste -sd,`,DefaultApi; \
		$(MOCKGEN) -destination ./pkg/mock/$$service/$$service.go -package $$service github.com/stackitcloud/stackit-sdk-go/services/$$service $$INTERFACES; \
	done
	@$(MOCKGEN) -destination ./pkg/stackit/iaas_mock.go -package stackit ./pkg/stackit IaasClient
	@$(MOCKGEN) -destination ./pkg/stackit/loadbalancer_mock.go -package stackit ./pkg/stackit LoadbalancerClient
	@$(MOCKGEN) -destination ./pkg/stackit/server_mock.go -package stackit ./pkg/stackit NodeClient
	@$(MOCKGEN) -destination ./pkg/stackit/metadata/metadata_mock.go -package metadata ./pkg/stackit/metadata IMetadata
	@$(MOCKGEN) -destination ./pkg/csi/util/mount/mount_mock.go -package mount ./pkg/csi/util/mount IMount

.PHONY: generate
generate: mocks
	go generate ./...
