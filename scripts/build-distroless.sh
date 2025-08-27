#!/bin/bash

# Build script for distroless multi-architecture images
# Usage: ./scripts/build-distroless.sh [tag] [push]

set -euo pipefail

# Configuration
IMAGE_NAME="${IMAGE_NAME:-vault-autounseal-operator}"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo 'dev')}"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse HEAD 2>/dev/null || echo 'unknown')}"
BUILD_DATE="${BUILD_DATE:-$(date -u +'%Y-%m-%dT%H:%M:%SZ')}"
TAG="${1:-${VERSION}}"
PUSH="${2:-false}"

# Platform support
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"
DOCKERFILE="${DOCKERFILE:-Dockerfile.distroless}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log() {
    echo -e "${BLUE}[BUILD]${NC} $1"
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

warn() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

# Validate prerequisites
check_prerequisites() {
    log "Checking prerequisites..."

    if ! command -v docker &> /dev/null; then
        error "Docker is not installed or not in PATH"
    fi

    # Check if buildx is available
    if ! docker buildx version &> /dev/null; then
        error "Docker buildx is required for multi-platform builds"
    fi

    # Create buildx instance if it doesn't exist
    if ! docker buildx inspect multiarch &> /dev/null; then
        log "Creating buildx instance for multi-platform builds..."
        docker buildx create --name multiarch --use --bootstrap
    else
        docker buildx use multiarch
    fi

    success "Prerequisites checked"
}

# Build the image
build_image() {
    log "Building distroless image..."
    log "Image: ${IMAGE_NAME}:${TAG}"
    log "Platforms: ${PLATFORMS}"
    log "Version: ${VERSION}"
    log "Git Commit: ${GIT_COMMIT}"
    log "Build Date: ${BUILD_DATE}"

    # Build arguments
    BUILD_ARGS=(
        --file "${DOCKERFILE}"
        --platform "${PLATFORMS}"
        --build-arg "VERSION=${VERSION}"
        --build-arg "GIT_COMMIT=${GIT_COMMIT}"
        --build-arg "BUILD_DATE=${BUILD_DATE}"
        --tag "${IMAGE_NAME}:${TAG}"
        --tag "${IMAGE_NAME}:latest"
    )

    # Add push flag if requested
    if [[ "${PUSH}" == "true" ]]; then
        BUILD_ARGS+=(--push)
        log "Image will be pushed to registry"
    else
        BUILD_ARGS+=(--load)
        warn "Image will be built locally only (use 'push' as second argument to push)"
    fi

    # Execute build
    docker buildx build "${BUILD_ARGS[@]}" .

    success "Image built successfully"
}

# Show image information
show_info() {
    if [[ "${PUSH}" != "true" ]]; then
        log "Image information:"
        docker images "${IMAGE_NAME}:${TAG}" --format "table {{.Repository}}\t{{.Tag}}\t{{.Size}}\t{{.CreatedAt}}"

        log "Testing image..."
        if docker run --rm "${IMAGE_NAME}:${TAG}" --help &> /dev/null; then
            success "Image runs successfully"
        else
            error "Image failed to run"
        fi
    fi
}

# Cleanup
cleanup() {
    log "Build completed"
}

# Main execution
main() {
    log "Starting distroless build for ${IMAGE_NAME}"

    check_prerequisites
    build_image
    show_info
    cleanup

    success "Build process completed!"
}

# Execute main function
main "$@"
