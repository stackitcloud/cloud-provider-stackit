TOOLS_BIN_DIR := hack/tools/bin
export PATH := $(abspath $(TOOLS_BIN_DIR)):$(PATH)

OS := $(shell uname -s | tr "[:upper:]" "[:lower:]")
ARCH := $(shell uname -m)

# renovate: datasource=github-releases depName=incu6us/goimports-reviser
GOIMPORTS_REVISER_VERSION ?= v3.10.0
# renovate: datasource=github-releases depName=golangci/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.6.0
# renovate: datasource=github-releases depName=uber-go/mock
MOCKGEN_VERSION ?= v0.6.0
# renovate: datasource=github-releases depName=chainguard-dev/apko
APKO_VERSION ?= v0.30.20
# renovate: datasource=github-releases depName=ko-build/ko
KO_VERSION ?= v0.18.0

KUBERNETES_TEST_VERSION ?= v1.33.5

# Tool targets should declare go.mod as a prerequisite, if the tool's version is managed via go modules. This causes
# make to rebuild the tool in the desired version, when go.mod is changed.
# For tools where the version is not managed via go.mod, we use a file per tool and version as an indicator for make
# whether we need to install the tool or a different version of the tool (make doesn't rerun the rule if the rule is
# changed).

# Use this "function" to add the version file as a prerequisite for the tool target: e.g.
#   $(KUBECTL): $(call tool_version_file,$(KUBECTL),$(KUBECTL_VERSION))
tool_version_file = $(TOOLS_BIN_DIR)/.version_$(subst $(TOOLS_BIN_DIR)/,,$(1))_$(2)

# This target cleans up any previous version files for the given tool and creates the given version file.
# This way, we can generically determine, which version was installed without calling each and every binary explicitly.
$(TOOLS_BIN_DIR)/.version_%:
	@version_file=$@; rm -f $${version_file%_*}*
	@touch $@

GOIMPORTS_REVISER := $(TOOLS_BIN_DIR)/goimports-reviser
$(GOIMPORTS_REVISER): $(call tool_version_file,$(GOIMPORTS_REVISER),$(GOIMPORTS_REVISER_VERSION))
	GOBIN=$(abspath $(TOOLS_BIN_DIR)) go install github.com/incu6us/goimports-reviser/v3@$(GOIMPORTS_REVISER_VERSION)

GOLANGCI_LINT := $(TOOLS_BIN_DIR)/golangci-lint
$(GOLANGCI_LINT): $(call tool_version_file,$(GOLANGCI_LINT),$(GOLANGCI_LINT_VERSION))
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(TOOLS_BIN_DIR) $(GOLANGCI_LINT_VERSION)

MOCKGEN := $(TOOLS_BIN_DIR)/mockgen
$(MOCKGEN): $(call tool_version_file,$(MOCKGEN),$(MOCKGEN_VERSION))
	GOBIN=$(abspath $(TOOLS_BIN_DIR)) go install go.uber.org/mock/mockgen@$(MOCKGEN_VERSION)

APKO := $(TOOLS_BIN_DIR)/apko
$(APKO): $(call tool_version_file,$(APKO),$(APKO_VERSION))
	GOBIN=$(abspath $(TOOLS_BIN_DIR)) go install chainguard.dev/apko@$(APKO_VERSION)

KO := $(TOOLS_BIN_DIR)/ko
$(KO): $(call tool_version_file,$(KO),$(KO_VERSION))
	GOBIN=$(abspath $(TOOLS_BIN_DIR)) go install github.com/google/ko@$(KO_VERSION)

KUBERNETES_TEST := $(TOOLS_BIN_DIR)/e2e.test
KUBERNETES_TEST_GINKGO := $(TOOLS_BIN_DIR)/ginkgo
$(KUBERNETES_TEST): $(call tool_version_file,$(KUBERNETES_TEST),$(KUBERNETES_TEST_VERSION))
	curl --location https://dl.k8s.io/$(KUBERNETES_TEST_VERSION)/kubernetes-test-$(OS)-$(ARCH).tar.gz | tar -C $(TOOLS_BIN_DIR) --strip-components=3 -zxf - kubernetes/test/bin/e2e.test kubernetes/test/bin/ginkgo
