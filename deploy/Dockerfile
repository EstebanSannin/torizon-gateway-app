# Torizon Gateway — multi-stage, multi-arch build.
# Primary target: linux/arm64 (Verdin / Zinnia). Also arm/v7, amd64 for dev.
# Build:  docker buildx build --platform linux/arm64,linux/amd64 -f deploy/Dockerfile -t torizon-gateway .

# ---- Build stage ----
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
WORKDIR /src
# Cache modules first.
COPY go.mod go.sum ./
RUN go mod download
# App sources (UI is embedded via //go:embed, so web/ must be present at build).
COPY . .
# GOARM comes from the platform variant (arm/v7 -> 7); empty/ignored otherwise.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM="${TARGETVARIANT#v}" \
    go build -trimpath -ldflags="-s -w" -o /out/gateway-manager ./cmd/gateway-manager

# ---- Runtime stage (minimal) ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/gateway-manager /usr/local/bin/gateway-manager
# Data volume (SQLite DB, TLS cert) — mounted by compose.
VOLUME ["/data"]
EXPOSE 8443
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/gateway-manager"]
