GO ?= go
NPM ?= npm
SRC = cmd internal
PKG = ./cmd/... ./internal/...

.PHONY: build backend frontend test test-go test-frontend clean docker

## build: build the gopad binary from the vendored frontend artifacts in
## internal/server/dist — no npm/node required. Only run `make frontend`
## (and commit the regenerated dist) when frontend sources change.
build: backend

backend:
	$(GO) build -o gopad ./cmd/gopad

## frontend: regenerate internal/server/dist from frontend/ sources.
## Requires node/npm (or run it in a node container).
frontend:
	cd frontend && $(NPM) install --no-audit --no-fund && $(NPM) run build

## test: run all Go tests (race detector on) and frontend tests
test: test-go test-frontend

test-go:
	@gofmt -w -s $(SRC)
	@$(GO) tool goimports -w $(SRC)
	@$(GO) vet $(PKG)
	@$(GO) test $(PKG)
	@$(GO) tool staticcheck $(PKG)
	@$(GO) tool govulncheck $(PKG)

test-frontend:
	cd frontend && $(NPM) run test

docker:
	docker build -t gopad .

clean:
	rm -f gopad
	rm -rf frontend/node_modules
