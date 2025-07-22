# OVN Metrics Collector

A standalone metrics collector that extracts and reuses the metrics collection functionality from ovn-kubernetes. This tool allows you to collect OVN (Open Virtual Network) and OVS (Open vSwitch) metrics independently of the main ovnkube binary.

## Features

- **OVS Metrics Collection**: Collects comprehensive Open vSwitch metrics including datapath, bridge, interface, and performance metrics
- **OVN Metrics Collection**: Collects OVN northbound database, southbound database, and ovn-controller metrics  
- **OVN Northd Metrics**: Collects ovn-northd daemon metrics and status information
- **Flexible Deployment**: Can run standalone, in containers, or as a Kubernetes DaemonSet
- **Prometheus Integration**: Exposes metrics in Prometheus format
- **TLS Support**: Optional TLS encryption for metrics endpoints
- **Health Monitoring**: Built-in health checks and pprof support

## Usage

### Develop
```bash
# Build the metrics collector binary
make build
```

### Basic Usage

```bash
# Start with default settings (OVS + OVN metrics)
./ovn-metrics-collector

# Start with custom ports
./ovn-metrics-collector \
  --metrics-bind-address="0.0.0.0:9476" \
  --ovn-metrics-bind-address="0.0.0.0:9310"

# Start with specific metrics collection
./ovn-metrics-collector \
  --export-ovs-metrics=true \
  --metrics-interval=30 \
  --node-name="worker-node-1"
```

### Command Line Options

| Flag | Description | Default |
|------|-------------|---------|
| `--metrics-bind-address` | Address for main metrics server | `0.0.0.0:9476` |
| `--ovn-metrics-bind-address` | Address for OVN-specific metrics server | `0.0.0.0:9310` |
| `--metrics-interval` | Metrics collection interval (seconds) | `30` |
| `--export-ovs-metrics` | Enable OVS metrics collection | `true` |
| `--enable-ovn-nb-db-metrics` | Enable OVN NB database metrics collection | `true` |
| `--enable-ovn-sb-db-metrics` | Enable OVN SB database metrics collection | `true` |
| `--enable-ovn-controller-metrics` | Enable OVN controller metrics collection | `true` |
| `--enable-ovn-northd-metrics` | Enable OVN northd metrics collection | `true` |
| `--enable-pprof` | Enable pprof endpoints | `false` |
| `--kubeconfig` | Path to kubeconfig file | Auto-detected |
| `--node-name` | Kubernetes node name | Auto-detected |
| `--server-cert` | TLS certificate file | None |
| `--server-key` | TLS private key file | None |
| `--ovn-run-dir` | OVN runtime directory (where ovn-northd.pid and control sockets are located) | `/var/run/ovn/` |
| `--loglevel` | Log verbosity level | `2` |

## Environment Variables

The collector respects the following environment variables:

- `NODE_NAME`: Kubernetes node name
- `HOSTNAME`: Fallback for node name detection
- `KUBERNETES_SERVICE_HOST`: Auto-detects in-cluster configuration

## Metrics Endpoints

### Main Metrics Server (default: :9476)
- `/metrics` - General Prometheus metrics
- `/debug/pprof/*` - pprof endpoints (if enabled)

### OVN Metrics Server (default: :9310)  
- `/metrics` - OVN-specific metrics (OVN DB, controller, northd)

## Available Metrics

### OVS Metrics
- **Datapath metrics**: Flow statistics, packet counters, mask statistics
- **Bridge metrics**: Flow counts, port counts, OpenFlow rule statistics  
- **Interface metrics**: RX/TX statistics, error counts, collision counts
- **Memory metrics**: Handler and revalidator thread counts
- **Hardware offload metrics**: TC policy and offload statistics
- **Coverage metrics**: Internal OVS operation counters

### OVN Metrics
- **Database metrics**: Connection status, transaction statistics
- **Controller metrics**: Flow counts, bridge port statistics
- **Northd metrics**: Status, configuration probe intervals
- **Build information**: Version details for all components

## Deployment

### Docker

```dockerfile
FROM ubuntu:22.04
RUN apt-get update && apt-get install -y openvswitch-switch
COPY ovn-metrics-collector /usr/bin/
ENTRYPOINT ["/usr/bin/ovn-metrics-collector"]
```

### Kubernetes DaemonSet

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: ovn-metrics-collector
spec:
  selector:
    matchLabels:
      app: ovn-metrics-collector
  template:
    metadata:
      labels:
        app: ovn-metrics-collector
    spec:
      hostNetwork: true
      hostPID: true
      containers:
      - name: metrics-collector
        image: ovn-metrics-collector:latest
        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        ports:
        - containerPort: 9476
          name: metrics
        - containerPort: 9310
          name: ovn-metrics
        volumeMounts:
        - name: ovs-run
          mountPath: /var/run/openvswitch
        - name: ovn-run
          mountPath: /var/run/ovn
      volumes:
      - name: ovs-run
        hostPath:
          path: /var/run/openvswitch
      - name: ovn-run
        hostPath:
          path: /var/run/ovn
```

## Building

### Prerequisites
- Go 1.23+
- Access to ovn-kubernetes source code
- OVS/OVN development libraries

### Build Steps

```bash
# Clone this project adjacent to ovn-kubernetes
git clone <this-repo> ovn-metrics-collector
cd ovn-metrics-collector

# Build the binary  
go mod tidy
go build -o ovn-metrics-collector .
```

### Cross-compilation

```bash
# For different architectures
GOOS=linux GOARCH=amd64 go build -o ovn-metrics-collector-linux-amd64 .
GOOS=linux GOARCH=arm64 go build -o ovn-metrics-collector-linux-arm64 .
```

## Configuration Examples

### Basic OVS-only Collection
```bash
./ovn-metrics-collector \
  --export-ovs-metrics=true \
  --enable-ovn-nb-db-metrics=false \
  --enable-ovn-sb-db-metrics=false \
  --enable-ovn-controller-metrics=false \
  --enable-ovn-northd-metrics=false \
  --ovn-metrics-bind-address="" \
  --kubeconfig=""
```

### OVN Controller Metrics Only
```bash
./ovn-metrics-collector \
  --export-ovs-metrics=false \
  --enable-ovn-nb-db-metrics=false \
  --enable-ovn-sb-db-metrics=false \
  --enable-ovn-controller-metrics=true \
  --enable-ovn-northd-metrics=false \
  --metrics-bind-address="" \
  --node-name="$(hostname)"
```

### OVN NB Database Metrics Only
```bash
./ovn-metrics-collector \
  --export-ovs-metrics=false \
  --enable-ovn-nb-db-metrics=true \
  --enable-ovn-sb-db-metrics=false \
  --enable-ovn-controller-metrics=false \
  --enable-ovn-northd-metrics=false \
  --node-name="$(hostname)"
```

### OVN SB Database Metrics Only
```bash
./ovn-metrics-collector \
  --export-ovs-metrics=false \
  --enable-ovn-nb-db-metrics=false \
  --enable-ovn-sb-db-metrics=true \
  --enable-ovn-controller-metrics=false \
  --enable-ovn-northd-metrics=false \
  --node-name="$(hostname)"
```

### Production Deployment with TLS
```bash
./ovn-metrics-collector \
  --server-cert=/etc/ssl/certs/metrics.crt \
  --server-key=/etc/ssl/private/metrics.key \
  --metrics-bind-address="0.0.0.0:9476" \
  --ovn-metrics-bind-address="0.0.0.0:9310"
```

### High-frequency Monitoring
```bash
./ovn-metrics-collector \
  --metrics-interval=10 \
  --enable-pprof=true \
  --loglevel=4
```

### Custom OVN Run Directory
```bash
# For systems where OVN stores runtime files in /tmp instead of /var/run/ovn
./ovn-metrics-collector \
  --ovn-run-dir="/tmp/" \
  --enable-ovn-northd-metrics=true \
  --enable-ovn-nb-db-metrics=false \
  --enable-ovn-sb-db-metrics=false \
  --enable-ovn-controller-metrics=false \
  --export-ovs-metrics=false
```

## Custom OVN Run Directory Support

The metrics collector supports configurable OVN runtime directories, which is useful when:
- OVN stores runtime files in non-standard locations (e.g., `/tmp` instead of `/var/run/ovn`)
- You want to collect metrics directly without Kubernetes pod dependency checks
- Running in containerized environments with custom mount points

### Bypassing Pod Checks

By default, the OVN northd metrics collection waits for specific Kubernetes pods to be running. When using a custom OVN run directory (different from `/var/run/ovn/`), the collector automatically:

1. **Bypasses pod checks** - Doesn't wait for `ovn-db-pod=true` pods to be running
2. **Uses custom socket paths** - Reads PID files and control sockets from your specified directory
3. **Serves on separate endpoint** - Uses a custom metrics registry to avoid conflicts

### Configuration

```bash
# Standard behavior (waits for pods, uses /var/run/ovn/)
./ovn-metrics-collector --enable-ovn-northd-metrics=true

# Custom behavior (bypasses pod checks, uses /tmp/)
./ovn-metrics-collector \
  --ovn-run-dir="/tmp/" \
  --enable-ovn-northd-metrics=true
```

### Usage Examples

**Collect only OVN DB metrics from custom directory:**
```bash
./ovn-metrics-collector \
  --ovn-run-dir="/tmp/" \
  --enable-ovn-nb-db-metrics=true \
  --enable-ovn-sb-db-metrics=true \
  --enable-ovn-northd-metrics=false \
  --enable-ovn-controller-metrics=false \
  --export-ovs-metrics=false
```

**Collect both northd and DB metrics from custom directory:**
```bash
./ovn-metrics-collector \
  --ovn-run-dir="/tmp/" \
  --enable-ovn-nb-db-metrics=true \
  --enable-ovn-sb-db-metrics=true \
  --enable-ovn-northd-metrics=true \
  --enable-ovn-controller-metrics=false \
  --export-ovs-metrics=false
```

### Available Metrics with Custom Run Directory

When using a custom run directory, the following OVN metrics are collected:

**OVN Northd Metrics:**
- `ovn_northd_build_info` - Version and library information
- `ovn_northd_status` - Northd status (0=standby, 1=active, 2=paused)  
- `ovn_northd_nb_connection_status` - Northbound database connection (0=disconnected, 1=connected)
- `ovn_northd_sb_connection_status` - Southbound database connection (0=disconnected, 1=connected)

**OVN Database Metrics:**
- `ovn_db_build_info` - Database server version and schema information
- `ovn_db_nb_connection_status` - Northbound database socket availability (0=unavailable, 1=available)
- `ovn_db_sb_connection_status` - Southbound database socket availability (0=unavailable, 1=available)

## Troubleshooting

### Common Issues

1. **Permission denied accessing OVS socket**
   - Solution: Run with appropriate permissions or in privileged container

2. **No metrics appearing**
   - Check if OVS/OVN processes are running
   - Verify socket paths are accessible
   - Check log output for errors

3. **Kubernetes client connection failures**
   - Ensure kubeconfig is valid
   - Check RBAC permissions if running in-cluster

### Debug Commands

```bash
# Test OVS connectivity
ovs-vsctl show

# Test OVN connectivity  
ovn-nbctl show
ovn-sbctl show

# Check metrics endpoints
curl http://localhost:9476/metrics
curl http://localhost:9310/metrics
```

## License

This project inherits the license from ovn-kubernetes.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make changes and add tests
4. Submit a pull request

## Relation to ovn-kubernetes

This project extracts and reuses metrics collection code from the ovn-kubernetes project. It uses the same underlying metrics libraries and collection mechanisms, allowing you to run metrics collection independently of the main ovnkube processes.
