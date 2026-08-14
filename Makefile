# Torizon Gateway — developer tasks.
BINARY      := gateway-manager
PKG         := ./cmd/gateway-manager
REGISTRY    ?= torizon
TAG         ?= dev
PLATFORMS   ?= linux/arm64,linux/amd64

.PHONY: run build test tidy image image-multiarch vendor-ui fmt vet clean

## run: build + run locally over HTTPS (data + cert in ./.localdata)
run:
	GATEWAY_DATA_DIR=$(PWD)/.localdata GATEWAY_LISTEN_ADDR=:8443 GATEWAY_DEV_MODE=1 \
		go run $(PKG)

## build: compile the binary for the host arch
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/$(BINARY) $(PKG)

test:
	go test ./...

tidy:
	go mod tidy

fmt:
	go fmt ./...

vet:
	go vet ./...

## image: single-arch local image
image:
	docker build -f deploy/Dockerfile -t $(REGISTRY)/$(BINARY):$(TAG) .

## image-multiarch: buildx for arm64 (Verdin/Zinnia) + amd64
image-multiarch:
	docker buildx build --platform $(PLATFORMS) -f deploy/Dockerfile \
		-t $(REGISTRY)/$(BINARY):$(TAG) .

## vendor-ui: download htmx + alpine into web/static/vendor (run once, commit them)
vendor-ui:
	curl -fsSL https://unpkg.com/htmx.org/dist/htmx.min.js -o web/static/vendor/htmx.min.js
	curl -fsSL https://unpkg.com/htmx-ext-sse/dist/sse.js -o web/static/vendor/htmx-ext-sse.js
	curl -fsSL https://unpkg.com/alpinejs/dist/cdn.min.js -o web/static/vendor/alpine.min.js
	@echo "Also vendor Inter WOFF2 into web/static/vendor/inter/ (see docs/DESIGN-SYSTEM.md)"

clean:
	rm -rf bin .localdata
