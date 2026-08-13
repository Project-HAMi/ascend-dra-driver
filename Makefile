# Copyright 2023 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

CONTAINER_TOOL ?= docker
CARGO    ?= cargo
MKDIR    ?= mkdir
TR       ?= tr
DIST_DIR ?= $(CURDIR)/dist

include $(CURDIR)/common.mk

BUILDIMAGE_TAG ?= golang$(GOLANG_VERSION)
BUILDIMAGE ?= $(IMAGE_NAME)-build:$(BUILDIMAGE_TAG)

CMDS := $(patsubst ./cmd/%/,%,$(sort $(dir $(wildcard ./cmd/*/))))
CMD_TARGETS := $(patsubst %,cmd-%, $(CMDS))
GO_PACKAGES := ./api/... ./cmd/... ./pkg/...

.PHONY: init-submodules
init-submodules:
	@./scripts/init-submodules.sh

.PHONY: submodules
submodules:
	git submodule update --init --recursive

CHECK_TARGETS := assert-fmt vet lint ineffassign misspell
MAKE_TARGETS := binaries build check vendor fmt test examples cmds coverage generate image \
	libvnpu-artifacts verify-libvnpu-artifacts verify-helm-chart \
	verify-helm-release-path submodules $(CHECK_TARGETS)

TARGETS := $(MAKE_TARGETS) $(CMD_TARGETS)

DOCKER_TARGETS := $(patsubst %,docker-%, $(TARGETS))
.PHONY: $(TARGETS) $(DOCKER_TARGETS)

# Force CGO to use the dcmi header shipped with ascend-common, avoiding
# conflicts with system-installed (often older) Ascend driver headers.
DCMI_INCLUDE := $(CURDIR)/third_party/mind-cluster/component/ascend-common/devmanager/dcmi
CGO_CFLAGS := -I$(DCMI_INCLUDE) $(CGO_CFLAGS)
export CGO_CFLAGS

GOOS ?= linux

binaries: cmds
ifneq ($(PREFIX),)
cmd-%: COMMAND_BUILD_OPTIONS = -o $(PREFIX)/$(*)
endif
cmds: $(CMD_TARGETS)
$(CMD_TARGETS): cmd-%:
	CGO_LDFLAGS_ALLOW='-Wl,--unresolved-symbols=ignore-in-object-files' GOOS=$(GOOS) \
		go build -gcflags="all=-N -l" -ldflags " \
			-X $(VERSION_PACKAGE).version=$(VERSION) \
			-X $(VERSION_PACKAGE).revision=$(REVISION) \
			-X $(VERSION_PACKAGE).buildDate=$(BUILD_DATE)" \
			$(COMMAND_BUILD_OPTIONS) $(MODULE)/cmd/$(*)

build:
	GOOS=$(GOOS) go build $(GO_PACKAGES)

examples: $(EXAMPLE_TARGETS)
$(EXAMPLE_TARGETS): example-%:
	GOOS=$(GOOS) go build ./examples/$(*)

all: check test build binary
check: $(CHECK_TARGETS)

# Update the vendor folder
vendor:
	go mod vendor

# Apply go fmt to the codebase
fmt:
	git ls-files '*.go' | xargs gofmt -s -l -w

assert-fmt:
	git ls-files '*.go' | xargs gofmt -s -l > fmt.out
	@if [ -s fmt.out ]; then \
		echo "\nERROR: The following files are not formatted:\n"; \
		cat fmt.out; \
		rm fmt.out; \
		exit 1; \
	else \
		rm fmt.out; \
	fi

ineffassign:
	ineffassign $(MODULE)/...

lint:
	golangci-lint run $(GO_PACKAGES)

misspell:
	misspell $(MODULE)/...

vet:
	go vet $(GO_PACKAGES)

# Ensure that all log calls support contextual logging. This remains a
# standalone target because the repository does not vendor hack/tools.
.PHONY: logcheck
logcheck:
	(cd hack/tools && GOBIN=$(PWD) go install sigs.k8s.io/logtools/logcheck)
	./logcheck -check-contextual -check-deprecations ./...

COVERAGE_FILE := coverage.out
test: build cmds
	go test -v -coverprofile=$(COVERAGE_FILE) $(GO_PACKAGES)

coverage: test
	cat $(COVERAGE_FILE) | grep -v "_mock.go" > $(COVERAGE_FILE).no-mocks
	go tool cover -func=$(COVERAGE_FILE).no-mocks

LIBVNPU_ARTIFACTS_DIR := $(DIST_DIR)/hami-vnpu-core
LIBVNPU_LIBRARY := $(LIBVNPU_ARTIFACTS_DIR)/libvnpu.so
LIBVNPU_PRELOAD := $(LIBVNPU_ARTIFACTS_DIR)/ld.so.preload

libvnpu-artifacts: submodules
	$(CARGO) build --release --manifest-path $(CURDIR)/hami-vnpu-core/Cargo.toml
	$(MKDIR) -p $(LIBVNPU_ARTIFACTS_DIR)
	cp $(CURDIR)/hami-vnpu-core/target/release/libvnpu.so $(LIBVNPU_LIBRARY)
	printf '%s\n' '/hami-vnpu-core/libvnpu.so' > $(LIBVNPU_PRELOAD)

verify-libvnpu-artifacts:
	test -s $(LIBVNPU_LIBRARY)
	grep -Fxq '/hami-vnpu-core/libvnpu.so' $(LIBVNPU_PRELOAD)

image: submodules verify-libvnpu-artifacts
	$(CONTAINER_TOOL) build \
		--build-arg GOLANG_VERSION="$(GOLANG_VERSION)" \
		--build-arg BASE_IMAGE="$(BASE_IMAGE)" \
		--build-arg GOPROXY="$(GOPROXY)" \
		--build-arg VERSION="$(VERSION)" \
		--build-arg REVISION="$(REVISION)" \
		--build-arg BUILD_DATE="$(BUILD_DATE)" \
		--tag "$(IMAGE_NAME):$(vVERSION)" \
		-f deployments/container/Dockerfile .

HELM_CHART_DIR := deployments/helm/ascend-dra-driver
HELM_CHART_NAME := ascend-dra-driver

verify-helm-chart:
	helm lint --strict $(HELM_CHART_DIR)
	@set -eu; \
		rendered="$$(mktemp -d)"; \
		trap 'rm -rf "$$rendered"' EXIT; \
		helm template $(HELM_CHART_NAME) $(HELM_CHART_DIR) \
			--namespace $(HELM_CHART_NAME) > "$$rendered/default.yaml"; \
		grep -Fq -- 'command: ["/usr/bin/ascend-dra-kubeletplugin"]' "$$rendered/default.yaml"; \
		grep -Fq -- '--feature-gates=HAMivNPUCore=true' "$$rendered/default.yaml"; \
		helm template $(HELM_CHART_NAME) $(HELM_CHART_DIR) \
			--namespace $(HELM_CHART_NAME) \
			--set kubeletPlugin.fullCardAndTraditionalVNPU.enabled=true \
			> "$$rendered/full-card-and-traditional-vnpu.yaml"; \
		grep -Fq -- '--feature-gates=HAMivNPUCore=false' \
			"$$rendered/full-card-and-traditional-vnpu.yaml"

verify-helm-release-path:
	@set -eu; \
	packages="$$(mktemp -d)"; \
	trap 'rm -rf "$$packages"' EXIT; \
	chart_version="$$(awk '$$1 == "version:" { print $$2; exit }' $(HELM_CHART_DIR)/Chart.yaml)"; \
	if [ -z "$$chart_version" ]; then echo 'failed to resolve chart version' >&2; exit 1; fi; \
	helm package $(HELM_CHART_DIR) --destination "$$packages" >/dev/null; \
	package_path="$$packages/$(HELM_CHART_NAME)-$$chart_version.tgz"; \
	test -f "$$package_path"; \
	helm show chart "$$package_path" >/dev/null; \
	grep -Fq 'uses: helm/chart-releaser-action@v1.6.0' .github/workflows/build-helm-release.yaml; \
	grep -Fq 'charts_dir: deployments/helm' .github/workflows/build-helm-release.yaml; \
	if grep -Eq '^[[:space:]]*skip_packaging:[[:space:]]*true' .github/workflows/build-helm-release.yaml; then echo 'chart-releaser skip_packaging must remain disabled' >&2; exit 1; fi; \
	show_line="$$(grep -n 'helm show chart' .github/workflows/build-helm-release.yaml | cut -d: -f1)"; \
	package_line="$$(grep -n 'helm package "$${{ env.CHART_PATH }}" --destination .cr-release-packages' .github/workflows/build-helm-release.yaml | cut -d: -f1)"; \
	if [ -z "$$show_line" ] || [ -z "$$package_line" ] || [ "$$show_line" -ge "$$package_line" ]; then echo 'GHCR existence check must precede packaging' >&2; exit 1; fi

generate: generate-deepcopy

generate-deepcopy: vendor
	for api in $(APIS); do \
		rm -f $(CURDIR)/api/$(VENDOR)/resource/$${api}/zz_generated.deepcopy.go; \
		controller-gen \
			object:headerFile=$(CURDIR)/hack/boilerplate.generatego.txt \
			paths=$(CURDIR)/api/$(VENDOR)/resource/$${api}/ \
			output:object:dir=$(CURDIR)/api/$(VENDOR)/resource/$${api}; \
	done

setup-e2e:
	test/e2e/setup-e2e.sh

test-e2e:
	test/e2e/e2e.sh

teardown-e2e:
	test/e2e/teardown-e2e.sh

# Generate an image for containerized builds
# Note: This image is local only
.PHONY: .build-image
.build-image: docker/Dockerfile.devel
	if [ x"$(SKIP_IMAGE_BUILD)" = x"" ]; then \
		$(CONTAINER_TOOL) build \
			--progress=plain \
			--build-arg GOLANG_VERSION="$(GOLANG_VERSION)" \
			--tag $(BUILDIMAGE) \
			-f $(^) \
			docker; \
	fi

ifeq ($(CONTAINER_TOOL),podman)
CONTAINER_TOOL_OPTS=-v $(PWD):$(PWD):Z
else
CONTAINER_TOOL_OPTS=-v $(PWD):$(PWD) --user $$(id -u):$$(id -g)
endif

$(DOCKER_TARGETS): docker-%: .build-image
	@echo "Running 'make $(*)' in container $(BUILDIMAGE)"
	$(CONTAINER_TOOL) run \
		--rm \
		-e HOME=$(PWD) \
		-e GOCACHE=$(PWD)/.cache/go \
		-e GOPATH=$(PWD)/.cache/gopath \
		$(CONTAINER_TOOL_OPTS) \
		-w $(PWD) \
		$(BUILDIMAGE) \
			make $(*)

# Start an interactive shell using the development image.
PHONY: .shell
.shell:
	$(CONTAINER_TOOL) run \
		--rm \
		-ti \
		-e HOME=$(PWD) \
		-e GOCACHE=$(PWD)/.cache/go \
		-e GOPATH=$(PWD)/.cache/gopath \
		$(CONTAINER_TOOL_OPTS) \
		-w $(PWD) \
		$(BUILDIMAGE)
