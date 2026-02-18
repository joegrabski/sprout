#!/bin/bash
set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( cd "$SCRIPT_DIR/../../.." && pwd )"

echo "Building sprout binary..."
cd "$PROJECT_ROOT/apps/sprout"
go build -o "$PROJECT_ROOT/sprout" ./cmd/sprout

echo ""
echo "Generating CLI documentation..."
cd "$SCRIPT_DIR"
go run generate-cli-docs.go

echo ""
echo "Generating configuration documentation..."
go run generate-config-docs.go

echo ""
echo "Documentation generated successfully!"
