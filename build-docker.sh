#!/bin/bash

set -e

echo "🔧 Building MCP over libp2p Bridge - Docker Deployment"
echo "=================================================="

echo "📦 Step 1: Building Linux binaries..."
echo "Building main application..."
GOOS=linux GOARCH=amd64 go build -o go-agent cmd/agent/main.go

echo "Building MCP server..."
GOOS=linux GOARCH=amd64 go build -o examples/mcp-server ./examples/mcp-server.go

echo "✅ Binaries built successfully"

echo "🐳 Step 2: Building Docker images..."
docker build -f Dockerfile.fast -t go-agent:latest .
echo "✅ Docker images built successfully"

echo "🚀 Step 3: Docker Compose options available:"
echo ""
echo "Standard deployment:"
echo "  docker-compose up --build -d"
echo ""

echo "🔍 Step 4: Verifying build..."
echo "Binary sizes:"
ls -lh go-agent examples/mcp-server

echo "Docker images:"
docker images | grep go-agent

echo ""
echo "✅ Build completed successfully!"
echo ""
echo "🎯 Ready for deployment:"
echo "  - Main application: go-agent (Linux binary)"
echo "  - MCP server: examples/mcp-server (Linux binary)"
echo "  - Docker image: go-agent:latest"
echo "  - SSE Transport: Enabled"
echo "  - STDIO Transport: Enabled"
echo ""
