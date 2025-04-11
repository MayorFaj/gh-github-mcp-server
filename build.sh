#!/bin/bash

# Build the extension
go build -ldflags="-X main.version=dev" -o gh-github-mcp-server

# Install it locally
echo "Installing extension locally..."
gh extension remove gh-github-mcp-server 2>/dev/null || true
gh extension install .

echo "Extension installed. Run with: gh github-mcp-server stdio"

gh github-mcp-server stdio
