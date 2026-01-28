#!/bin/bash
# Build script that injects version information

set -e

VERSION=$(cat VERSION 2>/dev/null || echo "dev")
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS="-X main.appVersion=${VERSION} -X main.buildTime=${BUILD_TIME} -X main.gitCommit=${GIT_COMMIT}"

echo "Building migsug version ${VERSION}..."
echo "Git commit: ${GIT_COMMIT}"
echo "Build time: ${BUILD_TIME}"

# Build for the platform specified or current platform
PLATFORM=${1:-$(go env GOOS)}
ARCH=${2:-$(go env GOARCH)}

# Create platform directory
PLATFORM_DIR="bin/${PLATFORM}-${ARCH}"
mkdir -p "$PLATFORM_DIR"

# Set output filename
OUTPUT="${PLATFORM_DIR}/migsug"
if [ "$PLATFORM" = "windows" ]; then
    OUTPUT="${PLATFORM_DIR}/migsug.exe"
fi

echo "Building for ${PLATFORM}/${ARCH}..."
GOOS=$PLATFORM GOARCH=$ARCH go build -ldflags "$LDFLAGS" -o "$OUTPUT" cmd/migsug/main.go

echo "Build complete: $OUTPUT"
ls -lh "$OUTPUT"
