GO_IMAGE ?= golang:1.26.5
GO_CONTAINER = docker run --rm \
	--user $$(id -u):$$(id -g) \
	-e GOCACHE=/tmp/go-build \
	-e GOMODCACHE=/tmp/go-mod \
	-e SOURCE_DATE_EPOCH \
	-v "$(CURDIR):/src" \
	-w /src \
	$(GO_IMAGE)

.PHONY: agent-evaluate agent-scorer-test agent-skill-test build docs-check e2e fmt images images-test install-test release release-build release-dry-run release-verify spike test test-race vet

agent-evaluate:
	bash tests/agents/evaluate.sh installed \
		--codex "$${CODEX_BIN:-$${HOME}/.local/bin/codex}" \
		--claude "$${CLAUDE_BIN:-$${HOME}/.local/bin/claude}" \
		--samples "$${AGENT_SAMPLES:-5}"

agent-scorer-test:
	bash tests/agents/evaluate.sh self-test

agent-skill-test:
	$(GO_CONTAINER) go test ./tests/agents ./internal/agentintegration -count=1

build:
	mkdir -p bin
	$(GO_CONTAINER) go build -o bin/wktbox ./cmd/wktbox

docs-check:
	$(GO_CONTAINER) go test ./tests -run Docs -count=1

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
	docker build -f images/webtop/Dockerfile -t wktbox/webtop:dev .
	docker build -t wktbox/gateway:dev images/gateway

images-test:
	bash tests/images/labels_test.sh

install-test:
	bash tests/install/unix_test.sh

e2e:
	bash tests/e2e/automatic_loopback.sh
	bash tests/e2e/two_worktrees.sh
	bash tests/e2e/cross_box_connections.sh

release:
	$(GO_CONTAINER) bash scripts/build.sh "$${WKTBOX_VERSION:-dev}"

release-build:
	./scripts/build.sh "$(VERSION)"

release-dry-run:
	test -n "$(VERSION)"
	SOURCE_DATE_EPOCH="$$(git log -1 --format=%ct)" \
		$(GO_CONTAINER) bash scripts/build.sh "$(VERSION)"
	sha256sum --check dist/checksums.txt
	bash tests/release/verifier_test.sh
	$(GO_CONTAINER) go test ./tests/release -count=1

release-verify:
	sha256sum --check dist/checksums.txt
