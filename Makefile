GO_IMAGE ?= golang:1.26.5
GO_CONTAINER = docker run --rm \
	--user $$(id -u):$$(id -g) \
	-e GOCACHE=/tmp/go-build \
	-e GOMODCACHE=/tmp/go-mod \
	-v "$(CURDIR):/src" \
	-w /src \
	$(GO_IMAGE)

.PHONY: build e2e fmt images release spike test test-race vet

build:
	mkdir -p bin
	$(GO_CONTAINER) go build -o bin/wktbox ./cmd/wktbox

fmt:
	$(GO_CONTAINER) sh -c 'gofmt -w $$(find . -name "*.go" -not -path "./.git/*")'

spike:
	bash tests/integration/spike.sh

test:
	$(GO_CONTAINER) go test ./...

test-race:
	$(GO_CONTAINER) go test -race ./...

vet:
	$(GO_CONTAINER) go vet ./...

images:
	docker build -t wktbox/webtop:dev images/webtop
	docker build -t wktbox/gateway:dev images/gateway

e2e:
	bash tests/e2e/two_worktrees.sh

release:
	$(GO_CONTAINER) bash scripts/build.sh "$${WKTBOX_VERSION:-dev}"
