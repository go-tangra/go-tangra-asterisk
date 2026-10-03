.PHONY: generate check-generated test test-race test-integration ui-install ui-check ui-build build build-no-ui test-e2e benchmark
GO ?= go
NPM ?= npm
export GOCACHE ?= /tmp/asterisk-go-build

generate:
	$(NPM) --prefix ui run gen:api
check-generated:
	cd ui && npx --no-install openapi-typescript ../api/openapi/asterisk.yaml -o /tmp/asterisk-schema.d.ts
	cmp ui/src/api/schema.d.ts /tmp/asterisk-schema.d.ts
ui-install:
	$(NPM) --prefix ui ci
ui-check:
	$(NPM) --prefix ui run lint
	$(NPM) --prefix ui run test:unit
ui-build:
	$(NPM) --prefix ui run build
build: ui-build
	mkdir -p bin
	$(GO) build -buildvcs=false -trimpath -tags ui -o bin/asterisksvc ./cmd/asterisksvc
build-no-ui:
	mkdir -p bin
	$(GO) build -buildvcs=false -trimpath -o bin/asterisksvc ./cmd/asterisksvc
test:
	$(GO) test ./...
test-race:
	$(GO) test -race ./...
test-integration:
	$(GO) test -race -tags integration ./tests/integration -count=1
test-e2e:
	$(NPM) --prefix ui run test:e2e
benchmark:
	$(GO) test -tags integration ./tests/integration -run TestReferencePerformance -count=1 -v
