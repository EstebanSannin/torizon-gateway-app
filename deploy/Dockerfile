# Torizon Gateway — multi-stage, multi-arch build.
# Primary target: linux/arm64 (Verdin / Zinnia). Also arm/v7, amd64 for dev.
# Build:  docker buildx build --platform linux/arm64,linux/amd64 -f deploy/Dockerfile -t torizon-gateway .

# ---- Build stage ----
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
# Cache modules first.
COPY go.mod ./
# COPY go.sum ./   # add once external deps land
RUN go mod download || true
# App sources (UI is embedded via //go:embed, so web/ must be present at build).
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/gateway-manager ./cmd/gateway-manager

# ---- Runtime stage (minimal) ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/gateway-manager /usr/local/bin/gateway-manager
# Data volume (SQLite DB, TLS cert) — mounted by compose.
VOLUME ["/data"]
EXPOSE 8443
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/gateway-manager"]
