package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli/v2"
	"k8s.io/klog/v2"
	kexec "k8s.io/utils/exec"

	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/libovsdb"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/metrics"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/util"

	//"github.com/spf13/afero"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var customOVNRunDir string
var customOVNRegistry = prometheus.NewRegistry()

// customRunOVNNorthAppCtl is a custom implementation that uses a configurable OVN run directory
func customRunOVNNorthAppCtl(ovnRunDir string, args ...string) (string, string, error) {
	var cmdArgs []string

	// Ensure the directory ends with a slash
	if ovnRunDir != "" && ovnRunDir[len(ovnRunDir)-1] != '/' {
		ovnRunDir += "/"
	}

	// Read the ovn-northd PID file
	/*pid, err := afero.ReadFile(afero.NewOsFs(), ovnRunDir+"ovn-northd.pid")
	if err != nil {
		return "", "", fmt.Errorf("failed to run the command since failed to get ovn-northd's pid: %v", err)
	}*/

	// Build the control socket path
	cmdArgs = []string{
		"-t",
		ovnRunDir + fmt.Sprintf("ovn-northd.1.ctl"),
	}
	cmdArgs = append(cmdArgs, args...)

	// Execute ovn-appctl command using kexec
	exec := kexec.New()
	cmd := exec.Command("ovn-appctl", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)

	err := cmd.Run()
	return strings.Trim(strings.TrimSpace(stdout.String()), "\""), stderr.String(), err
}

// customGetOvnNorthdVersionInfo gets OVN northd version info using custom run directory
func customGetOvnNorthdVersionInfo(ovnRunDir string) (string, string) {
	stdout, _, err := customRunOVNNorthAppCtl(ovnRunDir, "version")
	if err != nil {
		klog.Errorf("Failed to get ovn-northd version: %v", err)
		return "", ""
	}

	var ovnNorthdVersion, ovnNorthdOvsLibVersion string
	// the output looks like:
	// ovn-northd 20.06.0.86f64fc1
	// Open vSwitch Library 2.13.0.f945b5c5
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "ovn-northd ") {
			ovnNorthdVersion = strings.Fields(line)[1]
		} else if strings.HasPrefix(line, "Open vSwitch Library ") {
			ovnNorthdOvsLibVersion = strings.Fields(line)[3]
		}
	}
	return ovnNorthdVersion, ovnNorthdOvsLibVersion
}

// customGetOvnNorthdConnectionStatusInfo gets connection status using custom run directory
func customGetOvnNorthdConnectionStatusInfo(ovnRunDir string, command string) float64 {
	stdout, stderr, err := customRunOVNNorthAppCtl(ovnRunDir, command)
	if err != nil {
		klog.Errorf("Failed to get ovn-northd %s stderr(%s): (%v)", command, stderr, err)
		return -1
	}
	connectionStatusMap := map[string]float64{
		"not connected": 0,
		"connected":     1,
	}
	if value, ok := connectionStatusMap[stdout]; ok {
		return value
	}
	return -1
}

// customGetOvnDbVersionInfo gets OVN database version info using custom run directory
func customGetOvnDbVersionInfo(ovnRunDir string, enableNB, enableSB bool) (string, string, string) {
	// Ensure the directory ends with a slash
	if ovnRunDir != "" && ovnRunDir[len(ovnRunDir)-1] != '/' {
		ovnRunDir += "/"
	}

	var ovnDbVersion, nbDbSchemaVersion, sbDbSchemaVersion string

	// Try to get ovsdb-server version using ovn-appctl
	stdout, _, err := customRunOVNNorthAppCtl(ovnRunDir, "version")
	if err == nil && strings.HasPrefix(stdout, "ovsdb-server (Open vSwitch) ") {
		ovnDbVersion = strings.Fields(stdout)[3]
	}

	// Get NB schema version only if enabled
	if enableNB {
		sockPath := "unix:" + ovnRunDir + "ovnnb_db.sock"
		stdout, _, err = util.RunOVSDBClient("get-schema-version", sockPath, "OVN_Northbound")
		if err == nil {
			nbDbSchemaVersion = strings.TrimSpace(stdout)
		} else {
			klog.Errorf("OVN nbdb schema version can't be fetched from %s: %s", sockPath, err)
		}
	}

	// Get SB schema version only if enabled
	if enableSB {
		sockPath := "unix:" + ovnRunDir + "ovnsb_db.sock"
		stdout, _, err = util.RunOVSDBClient("get-schema-version", sockPath, "OVN_Southbound")
		if err == nil {
			sbDbSchemaVersion = strings.TrimSpace(stdout)
		} else {
			klog.Errorf("OVN sbdb schema version can't be fetched from %s: %s", sockPath, err)
		}
	}

	return ovnDbVersion, nbDbSchemaVersion, sbDbSchemaVersion
}

// customRegisterOVNNorthdMetrics registers OVN northd metrics without pod checks and using custom run directory
func customRegisterOVNNorthdMetrics(ovnRunDir string, stopChan <-chan struct{}) {
	klog.Infof("Registering OVN northd metrics with custom run directory: %s (bypassing pod checks)", ovnRunDir)

	// Get version information using custom run directory
	ovnNorthdVersion, ovnNorthdOvsLibVersion := customGetOvnNorthdVersionInfo(ovnRunDir)

	// Register build_info metric
	customOVNRegistry.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace: "ovn",
			Subsystem: "northd",
			Name:      "build_info",
			Help: "A metric with a constant '1' value labeled by version and library " +
				"from which ovn binaries were built",
			ConstLabels: prometheus.Labels{
				"version":         ovnNorthdVersion,
				"ovs_lib_version": ovnNorthdOvsLibVersion,
			},
		},
		func() float64 { return 1 },
	))

	// Register status metric
	customOVNRegistry.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace: "ovn",
			Subsystem: "northd",
			Name:      "status",
			Help:      "Specifies whether this instance of ovn-northd is standby(0) or active(1) or paused(2).",
		}, func() float64 {
			stdout, stderr, err := customRunOVNNorthAppCtl(ovnRunDir, "status")
			if err != nil {
				klog.Errorf("Failed to get ovn-northd status stderr(%s): (%v)", stderr, err)
				return -1
			}
			northdStatusMap := map[string]float64{
				"standby": 0,
				"active":  1,
				"paused":  2,
			}
			if strings.HasPrefix(stdout, "Status:") {
				output := strings.TrimSpace(strings.Split(stdout, ":")[1])
				if value, ok := northdStatusMap[output]; ok {
					return value
				}
			}
			return -1
		},
	))

	// Register nb_connection_status metric
	customOVNRegistry.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace: "ovn",
			Subsystem: "northd",
			Name:      "nb_connection_status",
			Help:      "Specifies nb-connection-status of ovn-northd, not connected(0) or connected(1).",
		}, func() float64 {
			return customGetOvnNorthdConnectionStatusInfo(ovnRunDir, "nb-connection-status")
		},
	))

	// Register sb_connection_status metric
	customOVNRegistry.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace: "ovn",
			Subsystem: "northd",
			Name:      "sb_connection_status",
			Help:      "Specifies sb-connection-status of ovn-northd, not connected(0) or connected(1).",
		}, func() float64 {
			return customGetOvnNorthdConnectionStatusInfo(ovnRunDir, "sb-connection-status")
		},
	))
}

// Define all the metric variables for custom registry
var (
	customMetricOVNDBSessions = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "jsonrpc_server_sessions",
		Help:      "Active number of JSON RPC Server sessions to the DB"},
		[]string{"db_name"},
	)

	customMetricOVNDBMonitor = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "ovsdb_monitors",
		Help:      "Number of OVSDB Monitors on the server"},
		[]string{"db_name"},
	)

	customMetricDBSize = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "db_size_bytes",
		Help:      "The size of the database file associated with the OVN DB component."},
		[]string{"db_name"},
	)

	// Cluster status metrics
	customMetricDBClusterCID = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "cluster_id",
		Help:      "A metric with a constant '1' value labeled by database name and cluster uuid"},
		[]string{"db_name", "cluster_id"},
	)

	customMetricDBClusterSID = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "cluster_server_id",
		Help: "A metric with a constant '1' value labeled by database name, cluster uuid " +
			"and server uuid"},
		[]string{"db_name", "cluster_id", "server_id"},
	)

	customMetricDBClusterServerStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "cluster_server_status",
		Help: "A metric with a constant '1' value labeled by database name, cluster uuid, server uuid " +
			"server status"},
		[]string{"db_name", "cluster_id", "server_id", "server_status"},
	)

	customMetricDBClusterServerRole = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "cluster_server_role",
		Help: "A metric with a constant '1' value labeled by database name, cluster uuid, server uuid " +
			"and server role"},
		[]string{"db_name", "cluster_id", "server_id", "server_role"},
	)

	customMetricDBClusterTerm = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "cluster_term",
		Help: "A metric that returns the current election term value labeled by database name, cluster uuid, and " +
			"server uuid"},
		[]string{"db_name", "cluster_id", "server_id"},
	)

	customMetricDBClusterServerVote = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "cluster_server_vote",
		Help: "A metric with a constant '1' value labeled by database name, cluster uuid, server uuid " +
			"and server vote"},
		[]string{"db_name", "cluster_id", "server_id", "server_vote"},
	)

	customMetricDBClusterElectionTimer = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "cluster_election_timer",
		Help: "A metric that returns the current election timer value labeled by database name, cluster uuid, " +
			"and server uuid"},
		[]string{"db_name", "cluster_id", "server_id"},
	)

	customMetricDBClusterLogIndexStart = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "cluster_log_index_start",
		Help: "A metric that returns the log entry index start value labeled by database name, cluster uuid, " +
			"and server uuid"},
		[]string{"db_name", "cluster_id", "server_id"},
	)

	customMetricDBClusterLogIndexNext = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "cluster_log_index_next",
		Help: "A metric that returns the log entry index next value labeled by database name, cluster uuid, " +
			"and server uuid"},
		[]string{"db_name", "cluster_id", "server_id"},
	)

	customMetricDBClusterLogNotCommitted = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "cluster_log_not_committed",
		Help: "A metric that returns the number of log entries not committed labeled by database name, cluster uuid, " +
			"and server uuid"},
		[]string{"db_name", "cluster_id", "server_id"},
	)

	customMetricDBClusterLogNotApplied = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "cluster_log_not_applied",
		Help: "A metric that returns the number of log entries not applied labeled by database name, cluster uuid, " +
			"and server uuid"},
		[]string{"db_name", "cluster_id", "server_id"},
	)

	customMetricDBClusterConnIn = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "cluster_inbound_connections_total",
		Help: "A metric that returns the total number of inbound connections to the server labeled by " +
			"database name, cluster uuid, and server uuid"},
		[]string{"db_name", "cluster_id", "server_id"},
	)

	customMetricDBClusterConnOut = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "cluster_outbound_connections_total",
		Help: "A metric that returns the total number of outbound connections from the server labeled by " +
			"database name, cluster uuid, and server uuid"},
		[]string{"db_name", "cluster_id", "server_id"},
	)

	customMetricDBClusterConnInErr = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "cluster_inbound_connections_error_total",
		Help: "A metric that returns the total number of failed inbound connections to the server labeled by " +
			" database name, cluster uuid, and server uuid"},
		[]string{"db_name", "cluster_id", "server_id"},
	)

	customMetricDBClusterConnOutErr = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ovn",
		Subsystem: "db",
		Name:      "cluster_outbound_connections_error_total",
		Help: "A metric that returns the total number of failed outbound connections from the server labeled by " +
			"database name, cluster uuid, and server uuid"},
		[]string{"db_name", "cluster_id", "server_id"},
	)
)

// CustomOVNDBClusterStatus represents cluster status information
type CustomOVNDBClusterStatus struct {
	cid             string
	sid             string
	status          string
	role            string
	vote            string
	term            float64
	electionTimer   float64
	logIndexStart   float64
	logIndexNext    float64
	logNotCommitted float64
	logNotApplied   float64
	connIn          float64
	connOut         float64
	connInErr       float64
	connOutErr      float64
}

// customDBProperties represents database properties for custom implementation
type customDBProperties struct {
	DbName string
	DbPath string
}

// customAppCtl runs appctl command for custom database
func (db *customDBProperties) customAppCtl(ovnRunDir string, timeout int, args ...string) (string, string, error) {
	var cmdArgs []string

	// Ensure the directory ends with a slash
	if ovnRunDir != "" && ovnRunDir[len(ovnRunDir)-1] != '/' {
		ovnRunDir += "/"
	}

	// Build the control socket path based on database type
	var ctlPath string
	if db.DbName == "OVN_Northbound" {
		ctlPath = ovnRunDir + "ovnnb_db.ctl"
	} else if db.DbName == "OVN_Southbound" {
		ctlPath = ovnRunDir + "ovnsb_db.ctl"
	} else {
		return "", "", fmt.Errorf("unknown database name: %s", db.DbName)
	}

	// Correct ovn-appctl syntax: ovn-appctl -t <socket_path> <command> [args]
	cmdArgs = []string{"-t", ctlPath}
	cmdArgs = append(cmdArgs, args...)

	// Execute ovn-appctl command with timeout context
	exec := kexec.New()
	cmd := exec.Command("ovn-appctl", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)

	// Execute the command (timeout handling is simplified since k8s.io/utils/exec doesn't support Process field)
	err := cmd.Run()
	return strings.TrimSpace(stdout.String()), stderr.String(), err
}

// customGetOVNDBClusterStatusInfo gets cluster status for custom implementation
func customGetOVNDBClusterStatusInfo(ovnRunDir string, timeout int, dbProperties *customDBProperties) (*CustomOVNDBClusterStatus, error) {
	var stdout, stderr string
	var err error

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovering from a panic while parsing the cluster/status output "+
				"for database %q: %v", dbProperties.DbName, r)
		}
	}()

	stdout, stderr, err = dbProperties.customAppCtl(ovnRunDir, timeout, "cluster/status", dbProperties.DbName)
	if err != nil {
		klog.Errorf("Failed to retrieve cluster/status info for database %q, stderr: %s, err: (%v)",
			dbProperties.DbName, stderr, err)
		return nil, err
	}

	clusterStatus := &CustomOVNDBClusterStatus{}
	for _, line := range strings.Split(stdout, "\n") {
		idx := strings.Index(line, ":")
		if idx == -1 {
			continue
		}
		switch line[:idx] {
		case "Cluster ID":
			// the value is of the format `45ef (45ef51b9-9401-46e7-810d-6db0fc344ea2)`
			fields := strings.Fields(line[idx+2:])
			if len(fields) > 1 {
				clusterStatus.cid = strings.Trim(fields[1], "()")
			}
		case "Server ID":
			fields := strings.Fields(line[idx+2:])
			if len(fields) > 1 {
				clusterStatus.sid = strings.Trim(fields[1], "()")
			}
		case "Status":
			clusterStatus.status = line[idx+2:]
		case "Role":
			clusterStatus.role = line[idx+2:]
		case "Term":
			if value, err := strconv.ParseFloat(line[idx+2:], 64); err == nil {
				clusterStatus.term = value
			}
		case "Vote":
			clusterStatus.vote = line[idx+2:]
		case "Election timer":
			if value, err := strconv.ParseFloat(line[idx+2:], 64); err == nil {
				clusterStatus.electionTimer = value
			}
		case "Log":
			// the value is of the format [2, 1108]
			values := strings.Split(strings.Trim(line[idx+2:], "[]"), ", ")
			if len(values) >= 2 {
				if value, err := strconv.ParseFloat(values[0], 64); err == nil {
					clusterStatus.logIndexStart = value
				}
				if value, err := strconv.ParseFloat(values[1], 64); err == nil {
					clusterStatus.logIndexNext = value
				}
			}
		case "Entries not yet committed":
			if value, err := strconv.ParseFloat(line[idx+2:], 64); err == nil {
				clusterStatus.logNotCommitted = value
			}
		case "Entries not yet applied":
			if value, err := strconv.ParseFloat(line[idx+2:], 64); err == nil {
				clusterStatus.logNotApplied = value
			}
		case "Connections":
			// db cluster with 1 member has empty Connections list
			if idx+2 >= len(line) {
				continue
			}
			// the value is of the format `->0000 (->56d7) <-46ac <-56d7`
			var connIn, connOut, connInErr, connOutErr float64
			for _, conn := range strings.Fields(line[idx+2:]) {
				if strings.HasPrefix(conn, "->") {
					connOut++
				} else if strings.HasPrefix(conn, "<-") {
					connIn++
				} else if strings.HasPrefix(conn, "(->") {
					connOutErr++
				} else if strings.HasPrefix(conn, "(<-") {
					connInErr++
				}
			}
			clusterStatus.connIn = connIn
			clusterStatus.connOut = connOut
			clusterStatus.connInErr = connInErr
			clusterStatus.connOutErr = connOutErr
		}
	}

	return clusterStatus, nil
}

// customOVNDBClusterStatusMetricsUpdater updates cluster status metrics
func customOVNDBClusterStatusMetricsUpdater(ovnRunDir string, dbProperties *customDBProperties) {
	clusterStatus, err := customGetOVNDBClusterStatusInfo(ovnRunDir, 5, dbProperties)
	if err != nil {
		klog.Errorf("Error getting OVN DB cluster status information: %v", err.Error())
		return
	}
	customMetricDBClusterCID.WithLabelValues(dbProperties.DbName, clusterStatus.cid).Set(1)
	customMetricDBClusterSID.WithLabelValues(dbProperties.DbName, clusterStatus.cid, clusterStatus.sid).Set(1)
	customMetricDBClusterServerStatus.WithLabelValues(dbProperties.DbName, clusterStatus.cid, clusterStatus.sid,
		clusterStatus.status).Set(1)
	customMetricDBClusterTerm.WithLabelValues(dbProperties.DbName, clusterStatus.cid, clusterStatus.sid).Set(clusterStatus.term)
	customMetricDBClusterServerRole.WithLabelValues(dbProperties.DbName, clusterStatus.cid, clusterStatus.sid,
		clusterStatus.role).Set(1)
	customMetricDBClusterServerVote.WithLabelValues(dbProperties.DbName, clusterStatus.cid, clusterStatus.sid,
		clusterStatus.vote).Set(1)
	customMetricDBClusterElectionTimer.WithLabelValues(dbProperties.DbName, clusterStatus.cid,
		clusterStatus.sid).Set(clusterStatus.electionTimer)
	customMetricDBClusterLogIndexStart.WithLabelValues(dbProperties.DbName, clusterStatus.cid,
		clusterStatus.sid).Set(clusterStatus.logIndexStart)
	customMetricDBClusterLogIndexNext.WithLabelValues(dbProperties.DbName, clusterStatus.cid,
		clusterStatus.sid).Set(clusterStatus.logIndexNext)
	customMetricDBClusterLogNotCommitted.WithLabelValues(dbProperties.DbName, clusterStatus.cid,
		clusterStatus.sid).Set(clusterStatus.logNotCommitted)
	customMetricDBClusterLogNotApplied.WithLabelValues(dbProperties.DbName, clusterStatus.cid,
		clusterStatus.sid).Set(clusterStatus.logNotApplied)
	customMetricDBClusterConnIn.WithLabelValues(dbProperties.DbName, clusterStatus.cid,
		clusterStatus.sid).Set(clusterStatus.connIn)
	customMetricDBClusterConnOut.WithLabelValues(dbProperties.DbName, clusterStatus.cid,
		clusterStatus.sid).Set(clusterStatus.connOut)
	customMetricDBClusterConnInErr.WithLabelValues(dbProperties.DbName, clusterStatus.cid,
		clusterStatus.sid).Set(clusterStatus.connInErr)
	customMetricDBClusterConnOutErr.WithLabelValues(dbProperties.DbName, clusterStatus.cid,
		clusterStatus.sid).Set(clusterStatus.connOutErr)
}

// customOVNDBSizeMetricsUpdater updates database size metrics
func customOVNDBSizeMetricsUpdater(dbProps *customDBProperties) {
	if size, err := customGetOvnDBSizeViaPath(dbProps); err != nil {
		klog.Errorf("Failed to update OVN DB size metric: %v", err)
	} else {
		customMetricDBSize.WithLabelValues(dbProps.DbName).Set(float64(size))
	}
}

// customGetOvnDBSizeViaPath gets database size via file path
func customGetOvnDBSizeViaPath(dbProperties *customDBProperties) (int64, error) {
	fileInfo, err := os.Stat(dbProperties.DbPath)
	if err != nil {
		return 0, fmt.Errorf("failed to find OVN DB database %s at path %s: %v",
			dbProperties.DbName, dbProperties.DbPath, err)
	}
	return fileInfo.Size(), nil
}

// customOVNDBMemoryMetricsUpdater updates memory metrics
func customOVNDBMemoryMetricsUpdater(ovnRunDir string, dbProperties *customDBProperties) {
	var stdout, stderr string
	var err error

	stdout, stderr, err = dbProperties.customAppCtl(ovnRunDir, 5, "memory/show")
	if err != nil {
		klog.Errorf("Failed retrieving memory/show output for %q, stderr: %s, err: (%v)",
			strings.ToUpper(dbProperties.DbName), stderr, err)
		return
	}
	for _, kvPair := range strings.Fields(stdout) {
		if strings.HasPrefix(kvPair, "monitors:") {
			// kvPair will be of the form monitors:2
			fields := strings.Split(kvPair, ":")
			if len(fields) > 1 {
				if value, err := strconv.ParseFloat(fields[1], 64); err == nil {
					customMetricOVNDBMonitor.WithLabelValues(dbProperties.DbName).Set(value)
				} else {
					klog.Errorf("Failed to parse the monitor's value %s to float64: err(%v)",
						fields[1], err)
				}
			}
		} else if strings.HasPrefix(kvPair, "sessions:") {
			// kvPair will be of the form sessions:2
			fields := strings.Split(kvPair, ":")
			if len(fields) > 1 {
				if value, err := strconv.ParseFloat(fields[1], 64); err == nil {
					customMetricOVNDBSessions.WithLabelValues(dbProperties.DbName).Set(value)
				} else {
					klog.Errorf("Failed to parse the sessions' value %s to float64: err(%v)",
						fields[1], err)
				}
			}
		}
	}
}

// customIsOvnDBFoundViaPath checks if database files exist
func customIsOvnDBFoundViaPath(dbProperties []*customDBProperties) bool {
	for _, dbProperty := range dbProperties {
		if _, err := customGetOvnDBSizeViaPath(dbProperty); err != nil {
			return false
		}
	}
	return true
}

// customResetOvnDbClusterMetrics resets all cluster metrics
func customResetOvnDbClusterMetrics() {
	customMetricDBClusterCID.Reset()
	customMetricDBClusterSID.Reset()
	customMetricDBClusterServerStatus.Reset()
	customMetricDBClusterTerm.Reset()
	customMetricDBClusterServerRole.Reset()
	customMetricDBClusterServerVote.Reset()
	customMetricDBClusterElectionTimer.Reset()
	customMetricDBClusterLogIndexStart.Reset()
	customMetricDBClusterLogIndexNext.Reset()
	customMetricDBClusterLogNotCommitted.Reset()
	customMetricDBClusterLogNotApplied.Reset()
	customMetricDBClusterConnIn.Reset()
	customMetricDBClusterConnOut.Reset()
	customMetricDBClusterConnInErr.Reset()
	customMetricDBClusterConnOutErr.Reset()
}

// customResetOvnDbSizeMetric resets size metrics
func customResetOvnDbSizeMetric() {
	customMetricDBSize.Reset()
}

// customResetOvnDbMemoryMetrics resets memory metrics
func customResetOvnDbMemoryMetrics() {
	customMetricOVNDBMonitor.Reset()
	customMetricOVNDBSessions.Reset()
}

// customRegisterOVNDBMetrics registers OVN database metrics without pod checks and using custom run directory
func customRegisterOVNDBMetrics(ovnRunDir string, enableNB, enableSB bool, stopChan <-chan struct{}) {
	if !enableNB && !enableSB {
		klog.Info("Both NB and SB database metrics are disabled, skipping database metrics registration")
		return
	}

	var enabledDatabases []string
	if enableNB {
		enabledDatabases = append(enabledDatabases, "NB")
	}
	if enableSB {
		enabledDatabases = append(enabledDatabases, "SB")
	}
	klog.Infof("Registering OVN database metrics for %s with custom run directory: %s (bypassing pod checks)",
		strings.Join(enabledDatabases, " and "), ovnRunDir)

	// Get version information using custom run directory
	ovnDbVersion, nbDbSchemaVersion, sbDbSchemaVersion := customGetOvnDbVersionInfo(ovnRunDir, enableNB, enableSB)

	// Build ConstLabels based on enabled databases
	constLabels := prometheus.Labels{
		"version": ovnDbVersion,
	}
	if enableNB {
		constLabels["nb_schema_version"] = nbDbSchemaVersion
	}
	if enableSB {
		constLabels["sb_schema_version"] = sbDbSchemaVersion
	}

	// Register build_info metric with version information
	customOVNRegistry.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace: "ovn",
			Subsystem: "db",
			Name:      "build_info",
			Help: "A metric with a constant '1' value labeled by ovsdb-server version and " +
				"enabled database schema versions",
			ConstLabels: constLabels,
		},
		func() float64 { return 1 },
	))

	// Ensure the directory ends with a slash
	if ovnRunDir != "" && ovnRunDir[len(ovnRunDir)-1] != '/' {
		ovnRunDir += "/"
	}

	// Create database properties for enabled databases
	var dbProperties []*customDBProperties
	if enableNB {
		nbProps := &customDBProperties{
			DbName: "OVN_Northbound",
			DbPath: "/etc/ovn/ovnnb_db.db",
		}
		dbProperties = append(dbProperties, nbProps)
	}
	if enableSB {
		sbProps := &customDBProperties{
			DbName: "OVN_Southbound",
			DbPath: "/etc/ovn/ovnsb_db.db",
		}
		dbProperties = append(dbProperties, sbProps)
	}

	if len(dbProperties) == 0 {
		klog.Errorf("Failed to init properties for enabled databases")
		return
	}

	// Register memory metrics
	customOVNRegistry.MustRegister(customMetricOVNDBMonitor)
	customOVNRegistry.MustRegister(customMetricOVNDBSessions)

	// Check if DB is clustered or not
	dbIsClustered := true
	_, stderr, err := dbProperties[0].customAppCtl(ovnRunDir, 5, "cluster/status", dbProperties[0].DbName)
	if err != nil && strings.Contains(stderr, "is not a valid command") {
		dbIsClustered = false
		klog.Info("Found db is standalone, don't register db_cluster metrics")
	}

	// Register cluster metrics if database is clustered
	if dbIsClustered {
		klog.Info("Found db is clustered, register db_cluster metrics")
		customOVNRegistry.MustRegister(customMetricDBClusterCID)
		customOVNRegistry.MustRegister(customMetricDBClusterSID)
		customOVNRegistry.MustRegister(customMetricDBClusterServerStatus)
		customOVNRegistry.MustRegister(customMetricDBClusterTerm)
		customOVNRegistry.MustRegister(customMetricDBClusterServerRole)
		customOVNRegistry.MustRegister(customMetricDBClusterServerVote)
		customOVNRegistry.MustRegister(customMetricDBClusterElectionTimer)
		customOVNRegistry.MustRegister(customMetricDBClusterLogIndexStart)
		customOVNRegistry.MustRegister(customMetricDBClusterLogIndexNext)
		customOVNRegistry.MustRegister(customMetricDBClusterLogNotCommitted)
		customOVNRegistry.MustRegister(customMetricDBClusterLogNotApplied)
		customOVNRegistry.MustRegister(customMetricDBClusterConnIn)
		customOVNRegistry.MustRegister(customMetricDBClusterConnOut)
		customOVNRegistry.MustRegister(customMetricDBClusterConnInErr)
		customOVNRegistry.MustRegister(customMetricDBClusterConnOutErr)
	}

	// Check if database files exist for size metrics
	dbFoundViaPath := customIsOvnDBFoundViaPath(dbProperties)
	if dbFoundViaPath {
		customOVNRegistry.MustRegister(customMetricDBSize)
	} else {
		klog.Infof("Unable to enable OVN DB size metric because no OVN DBs found")
	}

	// Start the periodic metrics updater
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// To update not only values but also labels for metrics, we use Reset() to delete previous labels+value
				if dbIsClustered {
					customResetOvnDbClusterMetrics()
				}
				if dbFoundViaPath {
					customResetOvnDbSizeMetric()
				}
				customResetOvnDbMemoryMetrics()

				// Update metrics for each enabled database
				for _, dbProperty := range dbProperties {
					if dbIsClustered {
						customOVNDBClusterStatusMetricsUpdater(ovnRunDir, dbProperty)
					}
					if dbFoundViaPath {
						customOVNDBSizeMetricsUpdater(dbProperty)
					}
					customOVNDBMemoryMetricsUpdater(ovnRunDir, dbProperty)
				}
			case <-stopChan:
				return
			}
		}
	}()

	klog.Infof("Custom OVN database metrics registered successfully for %s", strings.Join(enabledDatabases, " and "))
}

// startCustomOVNMetricsServer starts a metrics server using our custom registry
func startCustomOVNMetricsServer(bindAddress, certFile, keyFile string, stopChan <-chan struct{}, wg *sync.WaitGroup) {
	handler := promhttp.InstrumentMetricHandler(customOVNRegistry,
		promhttp.HandlerFor(customOVNRegistry, promhttp.HandlerOpts{}))
	mux := http.NewServeMux()
	mux.Handle("/metrics", handler)

	var server *http.Server
	wg.Add(1)
	go func() {
		defer wg.Done()
		klog.Infof("Starting custom OVN metrics server at address %q", bindAddress)
		var listenAndServe func() error
		if certFile != "" && keyFile != "" {
			server = &http.Server{
				Addr:    bindAddress,
				Handler: mux,
			}
			listenAndServe = func() error { return server.ListenAndServeTLS(certFile, keyFile) }
		} else {
			server = &http.Server{Addr: bindAddress, Handler: mux}
			listenAndServe = func() error { return server.ListenAndServe() }
		}

		errCh := make(chan error)
		go func() {
			errCh <- listenAndServe()
		}()
		var err error
		select {
		case err = <-errCh:
			err = fmt.Errorf("failed while running custom OVN metrics server at address %q: %w", bindAddress, err)
			klog.Error(err)
		case <-stopChan:
			klog.Infof("Stopping custom OVN metrics server at address %q", bindAddress)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				klog.Errorf("Error stopping custom OVN metrics server at address %q: %v", bindAddress, err)
			}
		}
	}()
}

// overrideOVNNorthdMetrics replaces the metrics registration to use our custom run directory
func overrideOVNNorthdMetrics(kubeClient kubernetes.Interface, nodeName string, ovnRunDir string, stopChan <-chan struct{}) {
	// Use our completely custom implementation that bypasses pod checks
	customRegisterOVNNorthdMetrics(ovnRunDir, stopChan)
}

func main() {
	app := &cli.App{
		Name:  "ovn-metrics-collector",
		Usage: "Standalone OVN/OVS metrics collector",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "metrics-bind-address",
				Usage: "The IP address and port for the metrics server to serve on",
				Value: "0.0.0.0:9476",
			},
			&cli.StringFlag{
				Name:  "ovn-metrics-bind-address",
				Usage: "The IP address and port for the OVN metrics server to serve on",
				Value: "0.0.0.0:9310",
			},
			&cli.IntFlag{
				Name:  "metrics-interval",
				Usage: "The interval in seconds at which metrics are collected",
				Value: 30,
			},
			&cli.BoolFlag{
				Name:  "export-ovs-metrics",
				Usage: "Export OVS metrics along with OVN metrics",
				Value: true,
			},
			&cli.BoolFlag{
				Name:  "enable-ovn-nb-db-metrics",
				Usage: "Enable OVN Northbound database metrics collection",
				Value: true,
			},
			&cli.BoolFlag{
				Name:  "enable-ovn-sb-db-metrics",
				Usage: "Enable OVN Southbound database metrics collection",
				Value: true,
			},
			&cli.BoolFlag{
				Name:  "enable-ovn-controller-metrics",
				Usage: "Enable OVN controller metrics collection",
				Value: true,
			},
			&cli.BoolFlag{
				Name:  "enable-ovn-northd-metrics",
				Usage: "Enable OVN northd metrics collection",
				Value: true,
			},
			&cli.BoolFlag{
				Name:  "enable-pprof",
				Usage: "Enable pprof endpoints on the metrics server",
				Value: false,
			},
			&cli.StringFlag{
				Name:  "kubeconfig",
				Usage: "Path to kubeconfig file (if not running in cluster)",
			},
			&cli.StringFlag{
				Name:  "node-name",
				Usage: "Kubernetes node name (will be detected if not provided)",
			},
			&cli.StringFlag{
				Name:  "server-cert",
				Usage: "Certificate file for TLS",
			},
			&cli.StringFlag{
				Name:  "server-key",
				Usage: "Private key file for TLS",
			},
			&cli.StringFlag{
				Name:  "ovn-run-dir",
				Usage: "OVN runtime directory (where ovn-northd.pid and control sockets are located)",
				Value: "/var/run/ovn/",
			},
			&cli.IntFlag{
				Name:  "loglevel",
				Usage: "Log verbosity level",
				Value: 2,
			},
		},
		Action: runMetricsCollector,
	}

	app.Before = func(ctx *cli.Context) error {
		var level klog.Level
		klog.SetOutput(os.Stderr)
		if err := level.Set(strconv.Itoa(ctx.Int("loglevel"))); err != nil {
			return fmt.Errorf("failed to set klog log level: %v", err)
		}
		return nil
	}

	if err := app.Run(os.Args); err != nil {
		klog.Exit(err)
	}
}

func runMetricsCollector(ctx *cli.Context) error {
	// Set up signal handling
	stopChan := make(chan struct{})
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	// Initialize exec helper
	if err := util.SetExec(kexec.New()); err != nil {
		return fmt.Errorf("failed to initialize exec helper: %v", err)
	}

	// Set custom OVN run directory
	customOVNRunDir = ctx.String("ovn-run-dir")
	if customOVNRunDir != "" {
		klog.Infof("Using custom OVN run directory: %s", customOVNRunDir)
	}

	var wg sync.WaitGroup

	// Create Kubernetes client if needed
	var kubeClient kubernetes.Interface
	if ctx.String("kubeconfig") != "" || (ctx.String("kubeconfig") == "" && os.Getenv("KUBERNETES_SERVICE_HOST") != "") {
		var config *rest.Config
		var err error

		if ctx.String("kubeconfig") != "" {
			config, err = clientcmd.BuildConfigFromFlags("", ctx.String("kubeconfig"))
		} else {
			config, err = rest.InClusterConfig()
		}

		if err != nil {
			klog.Warningf("Failed to create Kubernetes client: %v. Some metrics may not be available.", err)
		} else {
			kubeClient, err = kubernetes.NewForConfig(config)
			if err != nil {
				klog.Warningf("Failed to create Kubernetes clientset: %v. Some metrics may not be available.", err)
			}
		}
	}

	// Get node name
	nodeName := ctx.String("node-name")
	if nodeName == "" {
		nodeName = os.Getenv("NODE_NAME")
		if nodeName == "" {
			nodeName = os.Getenv("HOSTNAME")
		}
	}

	// Start OVS client
	ovsClient, err := libovsdb.NewOVSClient(stopChan)
	if err != nil {
		klog.Errorf("Failed to initialize OVS client: %v. OVS metrics will not be available.", err)
	}

	metricsScrapeInterval := ctx.Int("metrics-interval")

	// Register metrics based on what's available and enabled
	if ctx.Bool("export-ovs-metrics") && ovsClient != nil {
		klog.Info("Registering OVS metrics")
		metrics.RegisterOvsMetricsWithOvnMetrics(ovsClient, metricsScrapeInterval, stopChan)
	}

	// Register OVN metrics components individually based on flags
	if kubeClient != nil && nodeName != "" {
		enableNBMetrics := ctx.Bool("enable-ovn-nb-db-metrics")
		enableSBMetrics := ctx.Bool("enable-ovn-sb-db-metrics")

		if enableNBMetrics || enableSBMetrics {
			if customOVNRunDir != "/var/run/ovn/" {
				klog.Infof("Registering custom OVN database metrics with run directory: %s", customOVNRunDir)
				go customRegisterOVNDBMetrics(customOVNRunDir, enableNBMetrics, enableSBMetrics, stopChan)
			} else {
				// For standard metrics, we need to call the existing function which handles both databases
				// Note: The standard metrics.RegisterOvnDBMetrics doesn't support separate NB/SB flags
				if enableNBMetrics && enableSBMetrics {
					klog.Info("Registering standard OVN database metrics (both NB and SB)")
					go metrics.RegisterOvnDBMetrics(kubeClient, nodeName, stopChan)
				} else if enableNBMetrics || enableSBMetrics {
					klog.Warning("Standard OVN database metrics do not support separate NB/SB control. Use custom run directory for granular control.")
					klog.Info("Registering standard OVN database metrics (both NB and SB)")
					go metrics.RegisterOvnDBMetrics(kubeClient, nodeName, stopChan)
				}
			}
		}

		if ctx.Bool("enable-ovn-controller-metrics") && ovsClient != nil {
			klog.Info("Registering OVN controller metrics")
			go metrics.RegisterOvnControllerMetrics(ovsClient, metricsScrapeInterval, stopChan)
		}

		if ctx.Bool("enable-ovn-northd-metrics") {
			if customOVNRunDir != "/var/run/ovn/" {
				klog.Infof("Registering custom OVN northd metrics with run directory: %s", customOVNRunDir)
				go overrideOVNNorthdMetrics(kubeClient, nodeName, customOVNRunDir, stopChan)
			} else {
				klog.Info("Registering standard OVN northd metrics")
				go metrics.RegisterOvnNorthdMetrics(kubeClient, nodeName, stopChan)
			}
		}
	}

	// Start main metrics server
	if bindAddress := ctx.String("metrics-bind-address"); bindAddress != "" {
		klog.Infof("Starting main metrics server on %s", bindAddress)
		metrics.StartMetricsServer(
			bindAddress,
			ctx.Bool("enable-pprof"),
			ctx.String("server-cert"),
			ctx.String("server-key"),
			stopChan,
			&wg,
		)
	}

	// Start OVN metrics server
	if bindAddress := ctx.String("ovn-metrics-bind-address"); bindAddress != "" {
		// If we're using custom metrics (northd or db) with a custom run directory, use our custom server
		useCustomServer := (ctx.Bool("enable-ovn-northd-metrics") || ctx.Bool("enable-ovn-nb-db-metrics") || ctx.Bool("enable-ovn-sb-db-metrics")) &&
			customOVNRunDir != "/var/run/ovn/"

		if useCustomServer {
			klog.Infof("Starting custom OVN metrics server on %s (with custom run directory)", bindAddress)
			startCustomOVNMetricsServer(
				bindAddress,
				ctx.String("server-cert"),
				ctx.String("server-key"),
				stopChan,
				&wg,
			)
		} else {
			klog.Infof("Starting standard OVN metrics server on %s", bindAddress)
			metrics.StartOVNMetricsServer(
				bindAddress,
				ctx.String("server-cert"),
				ctx.String("server-key"),
				stopChan,
				&wg,
			)
		}
	}

	klog.Info("Metrics collector started successfully")

	// Wait for shutdown signal
	go func() {
		<-signalChan
		klog.Info("Received shutdown signal")
		close(stopChan)
	}()

	<-stopChan
	klog.Info("Shutting down...")
	wg.Wait()
	klog.Info("Shutdown complete")

	return nil
}
