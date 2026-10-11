.PHONY: generate check-generated test test-race test-integration ui-install ui-check ui-build build build-no-ui test-e2e benchmark packages packages-only
GO ?= go
NPM ?= npm
# nfpm is run at a pinned version.
NFPM ?= $(GO) run github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.47.0
# Package version (without "v") and architectures for `make packages`.
VERSION ?= $(shell git describe --tags --match 'v*' --always | sed 's/^v//')
PKG_ARCHES ?= amd64 arm64
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
# Standalone .deb and .rpm (packaging/nfpm.yaml) per PKG_ARCHES in dist/,
# with SHA256SUMS. `packages-only` reuses an existing ui/dist.
packages: ui-build packages-only
packages-only:
	rm -rf dist bin/pkg
	mkdir -p dist bin/pkg
	for arch in $(PKG_ARCHES); do \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$arch $(GO) build -buildvcs=false -trimpath -tags ui -o bin/pkg/asterisksvc ./cmd/asterisksvc || exit 1; \
		for fmt in deb rpm; do \
			VERSION=$(VERSION) ARCH=$$arch $(NFPM) package -f packaging/nfpm.yaml -p $$fmt -t dist/ || exit 1; \
		done; \
	done
	cd dist && sha256sum *.deb *.rpm > SHA256SUMS
