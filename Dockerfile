# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk update && apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
COPY p2p-forge/go.mod p2p-forge/go.sum ./p2p-forge/

# Download dependencies
RUN go mod download

# Copy source code
ARG CACHEBUST=1
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o praxis-agent ./agent/main.go

# Runtime stage
FROM alpine:latest

# Install Docker CLI and other required tools
RUN apk update && apk add --no-cache docker-cli wget ca-certificates

# Set working directory
WORKDIR /app

# Copy built binaries, configuration and certificates
COPY --from=builder /app/praxis-agent .
COPY --from=builder /app/configs ./configs
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Create required directories
RUN mkdir -p /data /app/data /app/examples /shared

# Expose ports
EXPOSE 8000 8001 4001 4002 8090 8091

# Run the application
CMD ["./praxis-agent"]
