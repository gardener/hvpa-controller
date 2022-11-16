# Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

VERSION             := $(shell cat VERSION)
PACKAGES            :="$(go list -e ./... | grep -vE '/tmp/|/vendor/')"
REGISTRY            := eu.gcr.io/gardener-project/gardener
REPO_ROOT           := $(shell dirname "$(realpath $(lastword $(MAKEFILE_LIST)))")

IMAGE_REPOSITORY    := $(REGISTRY)/hvpa-controller
IMAGE_TAG           := $(VERSION)

# Image URL to use all building/pushing image targets
IMG ?= $(IMAGE_REPOSITORY):$(IMAGE_TAG)

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest

## Tool Versions
KUSTOMIZE_VERSION ?= v3.8.7
CONTROLLER_TOOLS_VERSION ?= v0.9.2

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

include hack/tools.mk

all: manager

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

test: envtest manifests generate fmt vet ## Run tests.
	source <($(LOCALBIN)/setup-envtest use -p env 1.25.0); go test ./internal/... ./controllers/... ./utils/... -coverprofile cover.out
	@cd "$(REPO_ROOT)/apis/autoscaling" && go test ./v1alpha2

# Build manager binary
manager: generate fmt vet
	@env GO111MODULE=on GOFLAGS=-mod=vendor go build -o bin/manager main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet
	@env GO111MODULE=on GOFLAGS=-mod=vendor go run ./main.go --enable-detailed-metrics --logtostderr=true --v=2

# Install CRDs into a cluster
install: manifests
	kubectl apply -f config/crd/output/crds.yaml

.PHONY: check
check: $(GOLANGCI_LINT)
	go vet ${PACKAGES}
	go fmt ${PACKAGES}
	# TODO: Lint result temporarily ignored, due to a number of known source code quality issues. To be addressed in a separate change set.
	-@hack/check.sh --golangci-lint-config=./.golangci.yaml ./...

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests
	kubectl apply -f config/crd/output/crds.yaml
	kustomize build config/default | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	cd "$(REPO_ROOT)/apis/autoscaling" && $(CONTROLLER_GEN) crd paths="./..." output:crd:artifacts:config=../../config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=manager-role webhook paths="./controllers/..."
	kustomize build config/crd -o config/crd/output/crds.yaml

# Run go fmt against code
fmt:
	@env GO111MODULE=on GOFLAGS=-mod=vendor go fmt ./...

# Run go vet against code
vet:
	@env GO111MODULE=on GOFLAGS=-mod=vendor go vet ./...

# Generate code
generate: controller-gen
	cd "$(REPO_ROOT)/apis/autoscaling" && $(CONTROLLER_GEN) object:headerFile=../../hack/boilerplate.go.txt paths=./...

# Build the docker image
docker-build: test
	docker build . -t ${IMG}
	@echo "updating kustomize image patch file for manager resource"
	sed -i'' -e 's@image: .*@image: '"${IMG}"'@' ./config/default/manager_image_patch.yaml

# Push the docker image
docker-push:
	docker push ${IMG}

# Revendor
revendor:
	@cd "$(REPO_ROOT)/apis/autoscaling" && go mod tidy
	@env GO111MODULE=on go mod tidy
	@env GO111MODULE=on go mod vendor

##@ Build Dependencies

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
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
