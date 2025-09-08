#!/bin/bash

# Start MCP Filesystem server on port 3000
echo "Starting MCP Filesystem Server..."

# Create a test directory for the filesystem server
mkdir -p ./test-filesystem

# Install and run the MCP filesystem server
npx -y @modelcontextprotocol/server-filesystem@latest \
  --transport sse \
  --sse-url http://localhost:3000/mcp \
  ./test-filesystem ./shared

# Alternative: Run with stdio transport for testing
# npx -y @modelcontextprotocol/server-filesystem@latest ./test-filesystem ./shared