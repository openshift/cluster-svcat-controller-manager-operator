IMAGE ?= docker.io/openshift/origin-cluster-svcat-controller-manager-operator
TAG ?= latest
PROG  := cluster-svcat-controller-manager-operator
GOFLAGS :=

all: build build-image verify
.PHONY: all
build:
	GODEBUG=tls13=1 go build $(GOFLAGS) ./cmd/cluster-svcat-controller-manager-operator
.PHONY: build

image:
	docker build -t "$(IMAGE):$(TAG)" .
.PHONY: build-image

test: test-unit test-e2e
.PHONY: test

test-unit:
ifndef JUNITFILE
	go test $(GOFLAGS) -race ./...
else
ifeq (, $(shell which gotest2junit 2>/dev/null))
$(error gotest2junit not found! Get it by `go get -u github.com/openshift/release/tools/gotest2junit`.)
endif
	GOCACHE=off go test $(GOFLAGS) -race -json ./... | gotest2junit > $(JUNITFILE)
endif
.PHONY: test-unit

test-e2e:
	GOCACHE=off go test -v ./test/e2e/...
.PHONY: test-e2e

verify: verify-govet
	hack/verify-gofmt.sh
	hack/verify-generated-bindata.sh
.PHONY: verify

verify-govet:
	go vet $(GOFLAGS) ./...
.PHONY: verify-govet

clean:
	rm -- "$(PROG)"
.PHONY: clean

CRD_SCHEMA_GEN_VERSION := v1.0.0
crd-schema-gen:
	git clone -b $(CRD_SCHEMA_GEN_VERSION) --single-branch --depth 1 https://github.com/openshift/crd-schema-gen.git $(CRD_SCHEMA_GEN_GOPATH)/src/github.com/openshift/crd-schema-gen
	GOPATH=$(CRD_SCHEMA_GEN_GOPATH) GOBIN=$(CRD_SCHEMA_GEN_GOPATH)/bin go install $(CRD_SCHEMA_GEN_GOPATH)/src/github.com/openshift/crd-schema-gen/cmd/crd-schema-gen
.PHONY: crd-schema-gen

update-codegen-crds: CRD_SCHEMA_GEN_GOPATH :=$(shell mktemp -d)
update-codegen-crds: crd-schema-gen
	$(CRD_SCHEMA_GEN_GOPATH)/bin/crd-schema-gen --apis-dir vendor/github.com/openshift/api/operator/v1
.PHONY: update-codegen-crds
update-codegen: update-codegen-crds
.PHONY: update-codegen

verify-codegen-crds: CRD_SCHEMA_GEN_GOPATH :=$(shell mktemp -d)
verify-codegen-crds: crd-schema-gen
	$(CRD_SCHEMA_GEN_GOPATH)/bin/crd-schema-gen --apis-dir vendor/github.com/openshift/api/operator/v1 --verify-only
.PHONY: verify-codegen-crds
verify-codegen: verify-codegen-crds
.PHONY: verify-codegen

verify: verify-codegen
