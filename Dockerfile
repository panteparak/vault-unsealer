# Build the manager binary  
FROM golang:1.25.0-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
ARG GO_VERSION
ARG BUILD_VERSION
ARG BUILD_DATE
ARG VCS_REF
ARG VERSION

# Install ca-certificates for SSL/TLS operations during build
RUN apk --no-cache add ca-certificates git

WORKDIR /workspace

# Copy the Go Modules manifests first for better layer caching
COPY go.mod go.mod
COPY go.sum go.sum

# Download dependencies in a separate layer for better caching
RUN go mod download && go mod verify

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/ internal/

# Build with security flags and optimizations
# -s -w: Strip debug information to reduce binary size
# -buildvcs=false: Disable VCS stamping for reproducible builds  
# -trimpath: Remove absolute paths from binary for better security
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -a -ldflags="-s -w -extldflags '-static' \
    -X 'main.version=${VERSION}' \
    -X 'main.buildDate=${BUILD_DATE}' \
    -X 'main.gitCommit=${VCS_REF}'" \
    -buildvcs=false -trimpath \
    -o manager cmd/main.go

# Use distroless static image with SSL certificates
# gcr.io/distroless/static:nonroot includes ca-certificates for HTTPS calls
FROM gcr.io/distroless/static:nonroot

# Set working directory
WORKDIR /

# Copy the binary from builder stage
COPY --from=builder /workspace/manager .

# Run as non-root user (65532:65532)
USER 65532:65532

# Container metadata with build information
LABEL maintainer="vault-autounseal-operator" \
      description="Kubernetes operator for HashiCorp Vault auto-unsealing" \
      version="1.0.0" \
      org.opencontainers.image.title="Vault Auto-unseal Operator" \
      org.opencontainers.image.description="Kubernetes operator for HashiCorp Vault auto-unsealing" \
      org.opencontainers.image.version="1.0.0" \
      org.opencontainers.image.source="https://github.com/panteparak/vault-unsealer" \
      org.opencontainers.image.licenses="MIT"

# Use exec form for better signal handling
ENTRYPOINT ["/manager"]
