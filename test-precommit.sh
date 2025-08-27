#!/bin/bash

set -e

echo "🧪 Testing pre-commit hooks..."

echo "1. Testing go mod tidy..."
go mod tidy

echo "2. Testing go mod verify..."
go mod verify

echo "3. Testing go fmt..."
gofmt -w .

echo "4. Testing go vet..."
go vet ./...

echo "5. Testing go test (unit tests only)..."
go test -short ./internal/...

echo "✅ All pre-commit hooks passed!"
