# Quick Start Guide

This guide shows you how to quickly get started with the OVN Metrics Collector.

## Prerequisites

- Go 1.23+
- OVS/OVN running on your system
- Access to ovn-kubernetes source code

## 1. Setup and Build

```bash
# Clone this project adjacent to ovn-kubernetes
git clone <this-repo> ovn-metrics-collector
cd ovn-metrics-collector

# Build the project
make build

# Verify the build
./ovn-metrics-collector --help
```

## 2. Basic Usage Examples

### Example 1: Standalone OVS Metrics Only

```bash
# Run with just OVS metrics (no Kubernetes required)
./ovn-metrics-collector \
  --export-ovs-metrics=true \
  --enable-ovn-nb-db-metrics=false \
  --enable-ovn-sb-db-metrics=false \
  --enable-ovn-controller-metrics=false \
  --enable-ovn-northd-metrics=false \
  --ovn-metrics-bind-address="" \
  --kubeconfig="" \
  --metrics-bind-address="0.0.0.0:9476"

# Check metrics
curl http://localhost:9476/metrics | grep ovs_
```

### Example 2: Full OVN + OVS Metrics 

```bash
# Run with full metrics collection (requires Kubernetes access)
./ovn-metrics-collector \
  --export-ovs-metrics=true \
  --metrics-bind-address="0.0.0.0:9476" \
  --ovn-metrics-bind-address="0.0.0.0:9310" \
  --node-name="$(hostname)" \
  --kubeconfig="$HOME/.kube/config"

# Check both endpoints
curl http://localhost:9476/metrics | head -20
curl http://localhost:9310/metrics | head -20
```

### Example 3: Docker Deployment

```bash
# Build and run with Docker
make docker-build
docker run -d \
  --name ovn-metrics \
  --privileged \
  --network host \
  -v /var/run/openvswitch:/var/run/openvswitch:ro \
  -v /var/run/ovn:/var/run/ovn:ro \
  ovn-metrics-collector:latest

# Check container logs
docker logs ovn-metrics
```

### Example 4: OVN Controller Metrics Only

```bash
# Run with only OVN controller metrics (for debugging controller issues)
./ovn-metrics-collector \
  --export-ovs-metrics=false \
  --enable-ovn-nb-db-metrics=false \
  --enable-ovn-controller-metrics=true \
  --enable-ovn-northd-metrics=false \
  --metrics-bind-address="" \
  --node-name="$(hostname)"

# Check controller-specific metrics
curl http://localhost:9310/metrics | grep ovn_controller
```

### Example 5: OVN Database Metrics Only

```bash
# Run with only OVN database metrics (for DB monitoring)
./ovn-metrics-collector \
  --export-ovs-metrics=false \
  --enable-ovn-nb-db-metrics=true \
  --enable-ovn-sb-db-metrics=true \
  --enable-ovn-controller-metrics=false \
  --enable-ovn-northd-metrics=false \
  --node-name="$(hostname)"

# Check database-specific metrics
curl http://localhost:9310/metrics | grep ovn_db
```

### Example 6: Full Stack with Monitoring

```bash
# Run with Prometheus and Grafana
docker-compose up -d

# Access services:
# - Metrics: http://localhost:9476/metrics, http://localhost:9310/metrics  
# - Prometheus: http://localhost:9090
# - Grafana: http://localhost:3000 (admin/admin)
```

### Example 7: Custom OVN Run Directory

```bash
# For systems where OVN runtime files are in /tmp instead of /var/run/ovn
# This bypasses Kubernetes pod checks and directly collects metrics
./ovn-metrics-collector \
  --ovn-run-dir="/tmp/" \
  --enable-ovn-northd-metrics=true \
  --enable-ovn-nb-db-metrics=false \
  --enable-ovn-sb-db-metrics=false \
  --enable-ovn-controller-metrics=false \
  --export-ovs-metrics=false \
  --node-name="$(hostname)"

# Check northd metrics from custom directory
curl http://localhost:9310/metrics | grep ovn_northd

# Expected metrics:
# ovn_northd_build_info{ovs_lib_version="...",version="..."} 1
# ovn_northd_status 1
# ovn_northd_nb_connection_status 1  
# ovn_northd_sb_connection_status 1
```

**Key Benefits:**
- ✅ **No pod dependency** - Works without Kubernetes pods running
- ✅ **Custom socket paths** - Reads from your specified directory
- ✅ **Direct metrics** - Bypasses ovn-kubernetes pod checks
- ✅ **Faster startup** - No waiting for pod readiness

### Example 8: OVN Database Metrics Only

```bash
# For collecting only OVN database metrics from custom directory
./ovn-metrics-collector \
  --ovn-run-dir="/tmp/" \
  --enable-ovn-nb-db-metrics=true \
  --enable-ovn-sb-db-metrics=true \
  --enable-ovn-northd-metrics=false \
  --enable-ovn-controller-metrics=false \
  --export-ovs-metrics=false \
  --node-name="$(hostname)"

# Check database metrics from custom directory
curl http://localhost:9310/metrics | grep ovn_db

# Expected metrics:
# ovn_db_build_info{nb_schema_version="...",sb_schema_version="...",version="..."} 1
# ovn_db_nb_connection_status 1
# ovn_db_sb_connection_status 1
```

### Example 9: Combined OVN Northd + NB DB Metrics

```bash
# For collecting both northd and database metrics from custom directory
./ovn-metrics-collector \
  --ovn-run-dir="/tmp/" \
  --enable-ovn-nb-db-metrics=true \
  --enable-ovn-northd-metrics=true \
  --enable-ovn-controller-metrics=false \
  --export-ovs-metrics=false \
  --node-name="$(hostname)"

# Check all OVN metrics
curl http://localhost:9310/metrics | grep "ovn_"
```

## 3. Verifying Metrics Collection

### Check Available Metrics

```bash
# See all available metrics
curl -s http://localhost:9476/metrics | grep "^# HELP" | head -10
curl -s http://localhost:9310/metrics | grep "^# HELP" | head -10

# Count metrics by type
curl -s http://localhost:9476/metrics | grep "^ovs_" | wc -l
curl -s http://localhost:9310/metrics | grep "^ovn_" | wc -l
```

### Sample Metrics Output

**OVS Metrics (`localhost:9476`):**
```
# HELP ovs_datapath_total Number of datapaths
# TYPE ovs_datapath_total gauge
ovs_datapath_total 1

# HELP ovs_bridge_total Number of OVS bridges  
# TYPE ovs_bridge_total gauge
ovs_bridge_total 2

# HELP ovs_interface_rx_packets_total Number of received packets
# TYPE ovs_interface_rx_packets_total counter
ovs_interface_rx_packets_total{interface="br-int"} 12345
```

**OVN Metrics (`localhost:9310`):**
```
# HELP ovn_controller_integration_bridge_openflow_total Total OpenFlow flows
# TYPE ovn_controller_integration_bridge_openflow_total counter
ovn_controller_integration_bridge_openflow_total 456

# HELP ovn_northd_status OVN Northd status (0=standby, 1=active, 2=paused)
# TYPE ovn_northd_status gauge
ovn_northd_status 1
```

## 4. Kubernetes Deployment

```bash
# Deploy to Kubernetes
kubectl apply -f k8s-deployment.yaml

# Check deployment status
kubectl get daemonset -n kube-system ovn-metrics-collector
kubectl get pods -n kube-system -l app=ovn-metrics-collector

# Port forward for testing
kubectl port-forward -n kube-system ds/ovn-metrics-collector 9476:9476
curl http://localhost:9476/metrics
```

## 5. Prometheus Integration

### Prometheus Configuration

Add to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'ovn-metrics-collector'
    static_configs:
      - targets: ['localhost:9476', 'localhost:9310']
    scrape_interval: 30s
```

### Sample Queries

**Top OVS Interfaces by RX Packets:**
```promql
topk(10, ovs_interface_rx_packets_total)
```

**OVN Controller Status:**
```promql
ovn_controller_sb_connection_status
```

**Bridge Flow Counts:**
```promql
ovs_bridge_flows_total
```

## 6. Troubleshooting

### Common Issues

**1. Permission denied accessing OVS sockets**
```bash
# Check socket permissions
ls -la /var/run/openvswitch/
# Run with appropriate permissions
sudo ./ovn-metrics-collector
```

**2. No metrics appearing**
```bash
# Check if OVS is running
sudo ovs-vsctl show
# Check if OVN is running  
sudo ovn-nbctl show
# Check logs
./ovn-metrics-collector --loglevel=4
```

**3. Kubernetes connection issues**
```bash
# Test kubeconfig
kubectl get nodes
# Check RBAC permissions
kubectl auth can-i get pods --as=system:serviceaccount:kube-system:ovn-metrics-collector
```

### Debug Commands

```bash
# Test OVS connectivity
ovs-appctl version
ovs-ofctl dump-flows br-int

# Test OVN connectivity
ovn-appctl -t ovn-controller version
ovn-nbctl show

# Test metrics endpoints with verbose output
curl -v http://localhost:9476/metrics
```

## 7. Next Steps

- Integrate with your monitoring stack
- Set up alerting rules based on OVN/OVS metrics
- Create custom Grafana dashboards
- Deploy across your Kubernetes clusters

For more advanced configuration and deployment options, see the main README.md file.
