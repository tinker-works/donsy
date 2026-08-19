GO ?= go
DOCKER ?= docker
LINTER_IMAGE ?= golangci/golangci-lint:v2.12.2
LINT_SOURCE ?= $(if $(GO_MERGE_DOCKER_BIND_SOURCE),$(GO_MERGE_DOCKER_BIND_SOURCE),$(CURDIR))
GO_FILES := $(shell find . -type f -name '*.go' -not -path './vendor/*')

.PHONY: fmt fmt-check tidy tidy-check test cover coverage vet lint build

fmt:
	gofmt -w $(GO_FILES)

fmt-check:
	@test -z "$$(gofmt -l $(GO_FILES))"

tidy:
	$(GO) mod tidy

tidy-check:
	$(GO) mod tidy
	git diff --exit-code -- go.mod go.sum

test:
	$(GO) test ./...

cover coverage:
	$(GO) test ./... -coverprofile=coverage.out

vet:
	$(GO) vet ./...

lint:
	$(DOCKER) run --rm --volume "$(LINT_SOURCE):/src" --workdir /src --entrypoint golangci-lint $(LINTER_IMAGE) run ./...

build:
	$(GO) build ./...
