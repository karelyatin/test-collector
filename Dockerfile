# Build stage
FROM golang:1.23-alpine AS builder

# Install dependencies for building
RUN apk add --no-cache git make

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o ovn-metrics-collector .

# Runtime stage
FROM ubuntu:22.04

# Install OVS/OVN tools and dependencies
RUN apt-get update && \
    apt-get install -y \
    openvswitch-switch \
    openvswitch-common \
    ovn-common \
    ovn-central \
    ovn-host \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN groupadd -r ovnmetrics && useradd -r -g ovnmetrics ovnmetrics

# Copy binary from builder stage
COPY --from=builder /app/ovn-metrics-collector /usr/bin/ovn-metrics-collector

# Make binary executable
RUN chmod +x /usr/bin/ovn-metrics-collector

# Create required directories
RUN mkdir -p /var/run/openvswitch /var/run/ovn && \
    chown ovnmetrics:ovnmetrics /var/run/openvswitch /var/run/ovn

# Expose metrics ports
EXPOSE 9476 9310

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:9476/metrics || exit 1

# Set user
USER ovnmetrics

# Default command
ENTRYPOINT ["/usr/bin/ovn-metrics-collector"]
CMD ["--loglevel=2"] 