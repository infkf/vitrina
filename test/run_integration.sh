#!/bin/bash
# Build and run integration tests in Docker.
# Requires: docker daemon running, and a Linux vitrina binary.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> Building Linux binary..."
GOOS=linux GOARCH=amd64 go build -o vitrina .

echo "==> Building test image..."
docker build -t vitrina-integration -f test/Dockerfile .

echo "==> Running integration tests..."
docker run --rm --privileged \
    -v /var/run/docker.sock:/var/run/docker.sock \
    vitrina-integration

echo "==> Done."
