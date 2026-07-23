GO_IMAGE ?= golang:1.26.5
GO_CONTAINER = docker run --rm \
	--user $$(id -u):$$(id -g) \
	-e GOCACHE=/tmp/go-build \
	-e GOMODCACHE=/tmp/go-mod \
	-v "$(CURDIR):/src" \
	-w /src \
	$(GO_IMAGE)

.PHONY: build fmt spike test vet

build:
	mkdir -p bin
	$(GO_CONTAINER) go build -o bin/wktbox ./cmd/wktbox

fmt:
	$(GO_CONTAINER) sh -c 'gofmt -w $$(find . -name "*.go" -not -path "./.git/*")'

spike:
	bash tests/integration/spike.sh

test:
	$(GO_CONTAINER) go test ./...

vet:
	$(GO_CONTAINER) go vet ./...
