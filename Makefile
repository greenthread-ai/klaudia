# Test layers — see docs/testing.md. `make check` is what CI runs.
#
#   static    gofmt, go vet, CGO_ENABLED=0 build
#   unit      package tests under internal/ (race detector on)
#   hermetic  unit tests again, against a poisoned HOME
#   e2e       the real binary against a scripted model; no network, no credential
#   smoke     the real binary against a live model (needs a credential)
#   torture   the Phase 2 agent-loop torture test (credential, docker, python3)
#
#   cover     statement coverage of unit + e2e, merged, per package

GO ?= go

.PHONY: check static unit hermetic e2e smoke torture cover install

check: static unit hermetic e2e

static:
	@out="$$(gofmt -l cmd internal e2e)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	$(GO) vet ./...
	CGO_ENABLED=0 $(GO) build ./...

# atexit_sleep_ms=0: a race-built binary sleeps 1s on exit by default, and the
# lsp and mcp tests re-exec the test binary as a fake server — a second per
# server stop, 3s for one mcp test. The sleep only helps report races in
# goroutines still running at exit; the parent test binary still reports them.
unit:
	GORACE=atexit_sleep_ms=0 $(GO) test -race -count=1 ./internal/...

hermetic:
	scripts/hermetic.sh -count=1

e2e:
	$(GO) test -count=1 ./e2e/...

smoke:
	scripts/smoke.sh

torture:
	scripts/torture.sh

cover:
	scripts/cover.sh

install:
	CGO_ENABLED=0 $(GO) install ./cmd/klaudia
