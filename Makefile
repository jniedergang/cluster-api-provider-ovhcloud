
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.31.0

#
# Go.
#
GO_VERSION ?= 1.24.2

# Use GOPROXY environment variable if set
GOPROXY := $(shell go env GOPROXY)
ifeq ($(GOPROXY),)
GOPROXY := https://proxy.golang.org
endif
export GOPROXY

# Active module mode, as we use go modules to manage dependencies
export GO111MODULE=on

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Registry / images
TAG ?= dev
ARCH ?= $(shell go env GOARCH)
ALL_ARCH = amd64 arm64
REGISTRY ?= ghcr.io
ORG ?= rancher-sandbox
IMAGE_NAME ?= cluster-api-provider-ovhcloud
# Image URL to use all building/pushing image targets
IMG ?= $(REGISTRY)/$(ORG)/$(IMAGE_NAME)

# Allow overriding the imagePullPolicy
PULL_POLICY ?= Always

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Directories
ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
BIN_DIR := bin
TOOLS_DIR := hack/tools
TOOLS_BIN_DIR := $(abspath $(TOOLS_DIR)/$(BIN_DIR))

export PATH := $(abspath $(TOOLS_BIN_DIR)):$(PATH)

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: generate-modules
generate-modules: ## Run go mod tidy to ensure modules are up to date
	go mod tidy

## --------------------------------------
## Lint / Verify
## --------------------------------------

##@ lint and verify:

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

GOLANGCI_LINT_VERSION ?= v2.11.1
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT)
$(GOLANGCI_LINT): $(LOCALBIN)
	test -s $(LOCALBIN)/golangci-lint || GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: lint
lint: golangci-lint ## Run golangci-lint over the codebase.
	$(GOLANGCI_LINT) run -v --timeout 5m ./...

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint with --fix.
	$(GOLANGCI_LINT) run -v --timeout 5m --fix ./...

.PHONY: verify-modules
verify-modules: ## Verify go.mod is tidy.
	go mod tidy
	@if !(git diff --quiet HEAD -- go.sum go.mod); then \
		git diff -- go.mod go.sum; \
		echo "go module files are out of date, run 'go mod tidy'"; exit 1; \
	fi

.PHONY: verify-gen
verify-gen: generate ## Verify generated files are up to date.
	@if !(git diff --quiet HEAD -- api/); then \
		git diff -- api/; \
		echo "generated files are out of date, run 'make generate'"; exit 1; \
	fi

.PHONY: verify-manifests
verify-manifests: manifests ## Verify generated manifests are up to date.
	@if !(git diff --quiet HEAD -- config/); then \
		git diff -- config/; \
		echo "generated manifests are out of date, run 'make manifests'"; exit 1; \
	fi

.PHONY: verify
verify: verify-modules verify-gen verify-manifests lint ## Run all verification checks.

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /test/e2e) -coverprofile cover.out

##@ Build

.PHONY: build
build: generate fmt vet ## Build manager binary.
	go build -o bin/manager ./cmd/

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

.PHONY: docker-build
docker-build: ## Build container image with the manager.
	podman build --build-arg TARGETARCH=$(ARCH) . -t $(IMG)-$(ARCH):$(TAG)

.PHONY: docker-push
docker-push: ## Push container image with the manager.
	podman push $(IMG)-$(ARCH):$(TAG)

.PHONY: docker-build-all
docker-build-all: $(addprefix docker-build-,$(ALL_ARCH)) ## Build container images for all architectures.

docker-build-%:
	$(MAKE) ARCH=$* docker-build

.PHONY: docker-push-all
docker-push-all: $(addprefix docker-push-,$(ALL_ARCH)) docker-push-manifest ## Push images for all architectures + multi-arch manifest.

docker-push-%:
	$(MAKE) ARCH=$* docker-push

.PHONY: docker-push-manifest
docker-push-manifest: ## Push a multi-arch manifest combining all per-arch images.
	podman manifest create --amend $(IMG):$(TAG) $(addprefix $(IMG)-,$(addsuffix :$(TAG),$(ALL_ARCH)))
	podman manifest push --rm $(IMG):$(TAG) docker://$(IMG):$(TAG)

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

## --------------------------------------
## Release
## --------------------------------------

##@ release:

RELEASE_DIR := out

$(RELEASE_DIR):
	mkdir -p $(RELEASE_DIR)/

.PHONY: release-manifests
release-manifests: $(RELEASE_DIR) $(KUSTOMIZE) ## Build the manifests to publish with a release.
	$(KUSTOMIZE) build config/default > $(RELEASE_DIR)/infrastructure-components.yaml
	# Pin the image to the release tag (defaults to :dev for non-release builds).
	# The raw bundle is meant for users who don't override via Helm values.
	sed -i 's|image: ghcr.io/rancher-sandbox/cluster-api-provider-ovhcloud:dev|image: $(IMG):$(or $(RELEASE_TAG),dev)|g' $(RELEASE_DIR)/infrastructure-components.yaml
	cp metadata.yaml $(RELEASE_DIR)/metadata.yaml
	cp templates/cluster-template-rke2.yaml $(RELEASE_DIR)/cluster-template.yaml
	@for f in cluster-template-rke2-floatingip cluster-template-kubeadm; do \
		if [ -f templates/$$f.yaml ]; then cp templates/$$f.yaml $(RELEASE_DIR)/; fi; \
	done
	@if [ -f templates/clusterclass/rke2/clusterclass-ovhcloud-rke2.yaml ]; then \
		cp templates/clusterclass/rke2/clusterclass-ovhcloud-rke2.yaml $(RELEASE_DIR)/; \
	fi
	@if [ -f templates/capiprovider-ovhcloud.yaml ]; then \
		cp templates/capiprovider-ovhcloud.yaml $(RELEASE_DIR)/; \
	fi

RELEASE_TAG ?= $(shell git describe --abbrev=0 2>/dev/null)

.PHONY: release
release: clean-release ## Build a release: validate, tag, build artifacts.
	@if [ -z "$(RELEASE_TAG)" ]; then echo "RELEASE_TAG is not set"; exit 1; fi
	@if ! [ -z "$$(git status --porcelain)" ]; then echo "git working tree is not clean"; exit 1; fi
	$(MAKE) verify
	$(MAKE) test
	$(MAKE) release-manifests
	@echo "Release artifacts ready in $(RELEASE_DIR)/"
	@ls -la $(RELEASE_DIR)/

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest

## Tool Versions
KUSTOMIZE_VERSION ?= v5.4.0
CONTROLLER_TOOLS_VERSION ?= v0.16.5
# Pinned to a known-good pseudo-version. setup-envtest is published only as
# pseudo-versions (no semver tags). Newer commits bump the required Go (the
# 2025-11-04 commit moved to Go 1.25, the 2026-04-05 commit to Go 1.26),
# which breaks Go 1.24 builds. Bump together with the Go toolchain.
ENVTEST_VERSION ?= v0.0.0-20250827215931-c4304622a139

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@$(ENVTEST_VERSION)

##@ Cleanup

.PHONY: clean
clean: ## Remove generated binaries and other build files.
	rm -rf $(BIN_DIR)
	rm -rf $(TOOLS_BIN_DIR)

.PHONY: clean-release
clean-release: ## Remove the release folder
	rm -rf $(RELEASE_DIR)
