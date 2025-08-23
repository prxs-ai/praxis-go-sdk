# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o go-agent cmd/agent/main.go

# Build MCP server example
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o examples/mcp-server examples/mcp-server.go

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates curl nodejs npm

# Install MCP memory server
RUN npm install -g @modelcontextprotocol/server-memory

# Set working directory
WORKDIR /app

# Copy built binaries
COPY --from=builder /app/go-agent .
COPY --from=builder /app/examples/mcp-server ./examples/

# Create data directory
RUN mkdir -p /data

# Expose ports
EXPOSE 8000 8001 4001 4002 8090 8091

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:${HTTP_PORT:-8000}/health || exit 1

# Run the application
CMD ["./go-agent"]
