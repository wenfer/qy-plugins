# QY-DASH Plugin SDK Developer Guide

Comprehensive guide for building high-performance, crash-resilient backend and frontend plugins for the QY-DASH microkernel platform using the official Go Plugin SDK (`pkg/pluginsdk`) and Native ESM Frontend Loader.

---

## Table of Contents
1. [Architectural Overview](#1-architectural-overview)
   - [Microkernel Process Supervisor](#microkernel-process-supervisor)
   - [UDS IPC Binary Framing Protocol](#uds-ipc-binary-framing-protocol)
   - [Native ESM Frontend Dynamic Loading](#native-esm-frontend-dynamic-loading)
2. [Getting Started with Go Plugin SDK](#2-getting-started-with-go-plugin-sdk)
   - [Module Setup & Lifecycle Contracts](#module-setup--lifecycle-contracts)
   - [Supervisor Environment Variables](#supervisor-environment-variables)
   - [Bootstrap & Graceful Shutdown](#bootstrap--graceful-shutdown)
3. [HostContext Capabilities](#3-hostcontext-capabilities)
   - [Server Topology & Node Inspection](#server-topology--node-inspection)
   - [Real-Time Telemetry Stream Subscription](#real-time-telemetry-stream-subscription)
   - [Strong SQLite Storage Isolation](#strong-sqlite-storage-isolation)
   - [Notification Dispatching](#notification-dispatching)
   - [Structured Logging](#structured-logging)
4. [High-Throughput Data Processing Engine](#4-high-throughput-data-processing-engine)
   - [Worker Pool Architecture](#worker-pool-architecture)
   - [Zero-Allocation Buffer Reuse with sync.Pool](#zero-allocation-buffer-reuse-with-syncpool)
   - [EWMA Real-Time Rate Calculations](#ewma-real-time-rate-calculations)
   - [Telemetry Anomaly Detection & Alerts](#telemetry-anomaly-detection--alerts)
5. [Frontend Plugin Development](#5-frontend-plugin-development)
   - [window.__QY_PLUGIN_SDK__ API Reference](#window__qy_plugin_sdk__-api-reference)
   - [Extension Points & UI Slots](#extension-points--ui-slots)
   - [React 19 Compatibility & Error Boundaries](#react-19-compatibility--error-boundaries)
   - [Hot-Unmount & Cleanup Protocols](#hot-unmount--cleanup-protocols)
6. [Complete End-to-End Tutorial: Reference Plugin](#6-complete-end-to-end-tutorial-reference-plugin)
   - [Project Structure](#project-structure)
   - [Backend Implementation](#backend-implementation)
   - [Frontend ESM Module](#frontend-esm-module)
   - [Compiling, Packaging & Verifying](#compiling-packaging--verifying)

---

## 1. Architectural Overview

QY-DASH implements an out-of-process microkernel plugin architecture designed for mission-critical proxy cluster monitoring and traffic management. Plugins run as isolated operating system subprocesses communicating over Unix Domain Sockets (UDS) with binary stream framing, combined with browser-native ECMAScript Module (ESM) dynamic loading for zero-recompile UI extensibility.

```
+-----------------------------------------------------------------------------------------+
|                                      HOST PROCESS                                       |
|                                                                                         |
|  +-------------------------+      +---------------------------+      +---------------+  |
|  |     HTTP Server /       |      |     Plugin Supervisor     |      |  Marketplace  |  |
|  |     REST API Router     |<---->|  - Fork/Exec Process Mgt  |<---->|  - index.json |  |
|  |  /api/v1/plugins/*      |      |  - <10ms Crash Detection  |      |  - Releases   |  |
|  +-------------------------+      |  - Hot-Plug (Enable/Dis)  |      |  - SHA-256    |  |
|               ^                   +---------------------------+      |  - Mirrors    |  |
|               |                                 ^                    +---------------+  |
|               v                                 v                                       |
|  +-------------------------+      +---------------------------+                         |
|  |   Frontend ESM Engine   |      |      UDS IPC Server       |                         |
|  |  window.__QY_PLUGIN_SDK |<---->|   - 16B Binary Framing    |                         |
|  |  dynamic import()       |      |   - Handshake & Tokens    |                         |
|  +-------------------------+      |   - Request/Resp RPC      |                         |
|                                   |   - Metrics Event Stream  |                         |
|  +-------------------------+      +---------------------------+                         |
|  |    Host Context Hub     |                    ^                                       |
|  |  - probe.Manager Sub    |                    | Unix Domain Socket (UDS)              |
|  |  - SQLite Isol Storage  |                    | /tmp/qysock-*/<id>.sock               |
|  |  - Notification Dispatch|                    v                                       |
+--+-------------------------+--------------------+---------------------------------------+
                                                  |
                                                  v
+-------------------------------------------------+---------------------------------------+
|                              PLUGIN SUBPROCESS (Go)                                     |
|                                                                                         |
|  +-----------------------------------------------------------------------------------+  |
|  | pkg/pluginsdk                                                                     |  |
|  |  - UDS IPC Client & Frame Codec (16-byte fixed framing)                           |  |
|  |  - HostContext (ServerList, SubscribeMetrics, Isolated Storage KV, Notify)         |  |
|  |  - pluginsdk.Serve(plugin) Lifecycle Supervisor Runner                            |  |
|  +-----------------------------------------------------------------------------------+  |
|                                         ^                                               |
|                                         v                                               |
|  +-----------------------------------------------------------------------------------+  |
|  | Plugin Business Logic                                                             |  |
|  |  - High-throughput Worker Pool & sync.Pool Zero-Alloc Buffers                      |  |
|  |  - EWMA Traffic Rates & Anomaly Detection Alerts                                  |  |
|  |  - Snapshot Persistence in Isolated Storage                                       |  |
|  +-----------------------------------------------------------------------------------+  |
+-----------------------------------------------------------------------------------------+
```

### Microkernel Process Supervisor
- **Process Isolation**: Every plugin subprocess is spawned via `exec.CommandContext` with dedicated process groups (`Setpgid: true`) and stripped environment variables (sensitive host secrets such as admin passwords, JWT secrets, and SSL certificates are strictly scrubbed).
- **Sub-10ms Crash Detection**: The host supervisor monitors child process termination via asynchronous `cmd.Wait()` goroutines, detecting unexpected terminations within 1ms (SLA: <10ms, hard ceiling: <1s) without blocking or impairing host operations.
- **Circuit Breaker**: Repeated crash flapping trips a circuit breaker (default: 5 crashes within 5 minutes) with exponential backoff (1s, 2s, 4s, 8s up to 30s) to prevent CPU starvation.
- **Hot-Plug Lifecycle**: Plugins can be installed, enabled, disabled, reconfigured, and uninstalled at runtime with zero downtime and no host restarts.

### UDS IPC Binary Framing Protocol
Host and plugin exchange structured messages over a local Unix Domain Socket (`AF_UNIX`) using a fixed 16-byte length-prefixed binary header followed by a variable-length payload.

```
0                   1                   2                   3
0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                       Magic: 0x5159504C ("QYPL")              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|  Version (1B) | MsgType (1B)  |   Flags (1B)  |  Reserved(1B) |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                       Sequence ID (4B)                        |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                       Payload Length (4B)                     |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                       Payload Data (N Bytes...)               |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

- **Magic** (`0x5159504C`): Constant ASCII `"QYPL"` identifying genuine QY-DASH protocol frames. Corrupted magic triggers immediate connection termination.
- **Version** (`0x01`): Protocol specification version.
- **MsgType**:
  - `0x01` `TypeHandshakeReq` / `0x02` `TypeHandshakeResp`
  - `0x03` `TypePing` / `0x04` `TypePong`
  - `0x10` `TypeGetServerListReq` / `0x11` `TypeGetServerListResp`
  - `0x12` `TypeGetServerReq` / `0x13` `TypeGetServerResp`
  - `0x20` `TypeSubscribeMetricsReq` / `0x21` `TypeSubscribeMetricsResp` / `0x22` `TypeMetricsEvent`
  - `0x23` `TypeUnsubscribeMetricsReq` / `0x24` `TypeUnsubscribeMetricsResp`
  - `0x30` `TypeStorageGetReq` / `0x31` `TypeStorageGetResp`
  - `0x32` `TypeStorageSetReq` / `0x33` `TypeStorageSetResp`
  - `0x34` `TypeStorageDeleteReq` / `0x35` `TypeStorageDeleteResp`
  - `0x36` `TypeStorageListReq` / `0x37` `TypeStorageListResp`
  - `0x40` `TypeSendNotificationReq` / `0x41` `TypeSendNotificationResp`
  - `0x50` `TypeShutdownReq` / `0x51` `TypeShutdownResp`
- **Flags**: Bitmask indicating compression, error response, or priority flags.
- **Sequence ID**: Unique uint32 correlating concurrent asynchronous requests and responses.
- **Payload Length**: uint32 big-endian byte count of the following data body (max 16 MB).

### Native ESM Frontend Dynamic Loading
Frontend extensions are compiled into standalone standard ECMAScript Modules (`.js`). The host frontend dynamically loads plugin modules at runtime using native browser `import()` without requiring npm installation or host bundle rebuilding. Global host runtime dependencies (`React`, `ReactDOM`, Lucide icons, Tailwind components, and host API clients) are provided via `window.__QY_PLUGIN_SDK__`.

---

## 2. Getting Started with Go Plugin SDK

### Module Setup & Lifecycle Contracts
Import the official SDK into your plugin module:

```bash
go get qy-dash/pkg/pluginsdk
```

Every Go plugin implements the `pluginsdk.Plugin` interface:

```go
package main

import (
	"context"
	"log"

	"qy-dash/pkg/pluginsdk"
)

type MyPlugin struct {
	host   pluginsdk.HostContext
	logger pluginsdk.Logger
}

// Start is invoked once the UDS socket connection and handshake succeed.
// Background workers should be spawned here and Start should return promptly.
func (p *MyPlugin) Start(host pluginsdk.HostContext) error {
	p.host = host
	p.logger = host.Log()
	p.logger.Infof("Plugin %s started successfully", host.PluginID())

	// Example: Query host server topology
	servers, err := host.GetServerList(host.Context())
	if err != nil {
		p.logger.Errorf("Failed to query servers: %v", err)
		return err
	}
	p.logger.Infof("Host is currently monitoring %d servers", len(servers))

	return nil
}

// Stop is invoked during graceful shutdown (SIGTERM, SIGINT, or host ShutdownReq).
// Flush remaining buffers, stop worker pools, and release resources.
func (p *MyPlugin) Stop() error {
	p.logger.Infof("Plugin is shutting down cleanly")
	return nil
}

func main() {
	plugin := &MyPlugin{}
	if err := pluginsdk.Serve(plugin); err != nil {
		log.Fatalf("Plugin runtime error: %v", err)
	}
}
```

### Supervisor Environment Variables
When the QY-DASH supervisor launches your plugin binary, it sets the following standard environment variables:

| Variable | Description | Example |
|---|---|---|
| `QY_PLUGIN_ID` | Unique identifier matching `plugin.json` | `metrics-analyzer` |
| `QY_PLUGIN_SOCKET` | Path to host UDS socket | `/tmp/qysock-12345/metrics-analyzer.sock` |
| `QY_PLUGIN_TOKEN` | Ephemeral authentication secret for handshake | `e0f7c2a49b81...` |
| `QY_PLUGIN_DATA_DIR` | Dedicated persistent directory on disk for this plugin | `/var/lib/qy-dash/plugins/metrics-analyzer/data` |

Host secrets (passwords, tokens, database credentials) are stripped by the supervisor, guaranteeing least-privilege security.

### Bootstrap & Graceful Shutdown
`pluginsdk.Serve()` handles the complete operational lifecycle:
1. **Connect**: Establishes local Unix Domain Socket connection to `QY_PLUGIN_SOCKET`.
2. **Authenticate**: Sends `TypeHandshakeReq` with `QY_PLUGIN_TOKEN` and plugin version.
3. **Initialize**: Constructs `HostContext` and calls `plugin.Start(hostContext)`.
4. **Supervise**: Listens for OS termination signals (`SIGTERM`, `SIGINT`), host cooperative shutdown frames (`TypeShutdownReq`), and socket disconnects.
5. **Teardown**: Calls `plugin.Stop()` with a 3-second grace period timeout before process termination.

---

## 3. HostContext Capabilities

The `HostContext` interface provides access to host services and data streams.

```go
type HostContext interface {
	Context() context.Context
	PluginID() string
	DataDir() string
	Storage() Storage
	GetServerList(ctx context.Context) ([]Server, error)
	GetServer(ctx context.Context, serverID string) (*Server, error)
	SubscribeMetrics(ctx context.Context, opts SubscribeOptions, handler MetricsHandler) (MetricsSubscription, error)
	SendNotification(ctx context.Context, n Notification) error
	Log() Logger
}
```

### Server Topology & Node Inspection
Query registered proxy servers and node statuses:

```go
servers, err := host.GetServerList(ctx)
for _, s := range servers {
	p.logger.Infof("Server [%s] %s (%s) - Status: %s, Latency: %dms",
		s.ID, s.Name, s.Host, s.Status, s.PingLatencyMs)
}

// Fetch single server details
server, err := host.GetServer(ctx, "srv-us-east-01")
if err != nil {
	p.logger.Warnf("Server not found: %v", err)
}
```

### Real-Time Telemetry Stream Subscription
Subscribe to high-frequency live probe telemetry broadcasts across all or selected servers:

```go
subOpts := pluginsdk.SubscribeOptions{
	ServerIDs:  nil,  // nil subscribes to all servers; or []string{"srv-1", "srv-2"}
	BufferSize: 1024, // Internal ring buffer capacity
}

sub, err := host.SubscribeMetrics(ctx, subOpts, pluginsdk.MetricsHandlerFunc(func(event *pluginsdk.MetricsEvent) {
	// High-throughput telemetry callback
	p.workerPool.Ingest(event, nil)
}))
if err != nil {
	return fmt.Errorf("subscribe metrics: %w", err)
}

// When finished, cancel subscription
defer sub.Close()
```

Each `MetricsEvent` contains structured probe telemetry:
```go
type MetricsEvent struct {
	Server     Server      `json:"server"`
	Stats      *ProbeStats `json:"stats,omitempty"`
	Node       *XrayNode   `json:"node,omitempty"`
	Nodes      []*XrayNode `json:"nodes,omitempty"`

	// Fallback fields for synthetic test events
	ServerID   string      `json:"server_id,omitempty"`
	CPU        float64     `json:"cpu,omitempty"`
	BytesIn    int64       `json:"bytes_in,omitempty"`
	BytesOut   int64       `json:"bytes_out,omitempty"`
}
```

Detailed real-time probe telemetry rates and hardware metrics reside in `event.Stats *ProbeStats`:
- `event.Stats.NetDownloadSpeed`: Real-time incoming network bandwidth (bytes/sec)
- `event.Stats.NetUploadSpeed`: Real-time outgoing network bandwidth (bytes/sec)
- `event.Stats.NetTotalRecv` / `event.Stats.NetTotalSent`: Total byte counters
- `event.Stats.CPUPercent` / `event.Stats.MemPercent`: Live resource percentages
- `event.Stats.TCPConnCount`: Active TCP socket connections

### Strong SQLite Storage Isolation
The host maintains an isolated Key-Value store backed by SQLite table `plugin_storage` with compound primary key `(plugin_id, key)`. The host IPC layer strictly binds `plugin_id` to the authenticated process token; plugins cannot read, overwrite, or enumerate another plugin's or host's keyspace.

```go
storage := host.Storage()

// 1. Raw string operations
err := storage.Set(ctx, "last_backup_time", "2026-11-14T10:00:00Z")
val, found, err := storage.Get(ctx, "last_backup_time")

// 2. Strongly-typed JSON serialization helpers
type AnalyzerState struct {
	ProcessedPackets uint64    `json:"processed_packets"`
	LastSpikeTime    time.Time `json:"last_spike_time"`
	CurrentRateMbps  float64   `json:"current_rate_mbps"`
}

state := AnalyzerState{
	ProcessedPackets: 154200,
	LastSpikeTime:    time.Now().UTC(),
	CurrentRateMbps:  48.75,
}

// Persist JSON
if err := storage.SetJSON(ctx, "state", state); err != nil {
	p.logger.Errorf("Storage SetJSON failed: %v", err)
}

// Retrieve JSON
var restored AnalyzerState
found, err := storage.GetJSON(ctx, "state", &restored)
if found {
	p.logger.Infof("Restored state: packets=%d, rate=%.2f Mbps",
		restored.ProcessedPackets, restored.CurrentRateMbps)
}

// 3. Prefix scanning & key listing
keys, err := storage.Keys(ctx, "stats:")
items, err := storage.List(ctx, "stats:")
for k, v := range items {
	fmt.Printf("Item %s = %s\n", k, v)
}

// 4. Deletion
err = storage.Delete(ctx, "temp_key")
```

### Notification Dispatching
Trigger system-wide administrative alerts via Telegram, DingTalk, Feishu, Webhook, and email channels configured in the host:

```go
notification := pluginsdk.Notification{
	Title:    "Traffic Spike Detected",
	Message:  "Server [us-east-node] exceeded EWMA threshold: 142.50 MB/s",
	ServerID: "us-east-node",
	Details: map[string]any{
		"level":    "warning", // "info", "warning", "error", "critical"
		"category": "telemetry",
		"tags":     []string{"bandwidth", "anomaly", "us-east"},
	},
}

if err := host.SendNotification(ctx, notification); err != nil {
	p.logger.Errorf("Failed to dispatch alert notification: %v", err)
}
```

### Structured Logging
Scoped logger prefixing all output with `[<plugin_id>]` and streaming to supervisor logs:

```go
logger := host.Log()
logger.Debugf("Parsed frame seq=%d len=%d", seq, length)
logger.Infof("Worker pool initialized with %d routines", workers)
logger.Warnf("Queue approaching 80%% capacity (%d/%d)", qLen, qCap)
logger.Errorf("Database flush failed: %v", err)
```

---

## 4. High-Throughput Data Processing Engine

When processing real-time telemetry from thousands of servers, garbage collection pauses and buffer allocations can create latency spikes. The architecture below combines worker pools, `sync.Pool` zero-allocation recyclers, and Exponentially Weighted Moving Average (EWMA) rates.

### Worker Pool Architecture
Decouple fast socket ingestion from computational analysis using a buffered ring worker pool.

```
+-----------------------------------------------------------------------------------------+
|                                    WORKER POOL PIPELINE                                 |
|                                                                                         |
|  [ UDS Socket ] ---> host.SubscribeMetrics()                                            |
|                              |                                                          |
|                              v (Zero Alloc)                                             |
|                     [ metricRecycler.Get() ]                                            |
|                              |                                                          |
|                              v                                                          |
|                     [ Channel Task Queue ]  (Bounded Ring Buffer)                       |
|                              |                                                          |
|         +--------------------+--------------------+                                     |
|         |                    |                    |                                     |
|         v                    v                    v                                     |
|   +------------+       +------------+       +------------+                              |
|   |  Worker 1  |       |  Worker 2  |       |  Worker N  |                              |
|   +------------+       +------------+       +------------+                              |
|         |                    |                    |                                     |
|         v                    v                    v                                     |
|   [ EWMA Rate Calc ]   [ Anomaly Spike ]    [ JSON Serializer ]                         |
|         |                    |                    |                                     |
|         +--------------------+--------------------+                                     |
|                              |                                                          |
|                              +------------------------> [ host.Storage().SetJSON() ]    |
|                              | (Spike Alert)                                            |
|                              +------------------------> [ host.SendNotification() ]     |
|                              |                                                          |
|                              v                                                          |
|                     [ metricRecycler.Put() ] (Recycled back to sync.Pool)               |
+-----------------------------------------------------------------------------------------+
```

```go
type WorkerPool struct {
	tasks      chan *NormalizedMetric
	numWorkers int
	pools      *MemoryPools
	handler    func(*NormalizedMetric)
	logger     pluginsdk.Logger
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

func NewWorkerPool(cfg Config, pools *MemoryPools, handler func(*NormalizedMetric), logger pluginsdk.Logger) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	wp := &WorkerPool{
		tasks:      make(chan *NormalizedMetric, cfg.QueueCapacity),
		numWorkers: cfg.NumWorkers,
		pools:      pools,
		handler:    handler,
		logger:     logger,
		ctx:        ctx,
		cancel:     cancel,
	}

	for i := 0; i < cfg.NumWorkers; i++ {
		wp.wg.Add(1)
		go wp.workerLoop(i)
	}
	return wp
}

func (wp *WorkerPool) Ingest(event *pluginsdk.MetricsEvent, raw []byte) bool {
	if event == nil {
		return false
	}

	// Allocate normalized metric from pool (zero heap allocation)
	norm := wp.pools.GetMetric()
	if event.Stats != nil && event.Stats.ServerID != "" {
		norm.ServerID = event.Stats.ServerID
		norm.Timestamp = event.Stats.Timestamp
		norm.CPUPercent = event.Stats.CPUPercent
		norm.MemPercent = event.Stats.MemPercent
		norm.TotalRecvBytes = event.Stats.NetTotalRecv
		norm.TotalSentBytes = event.Stats.NetTotalSent
		if event.Stats.NetDownloadSpeed > 0 {
			norm.DownloadRateMB = float64(event.Stats.NetDownloadSpeed) / (1024.0 * 1024.0)
		}
		if event.Stats.NetUploadSpeed > 0 {
			norm.UploadRateMB = float64(event.Stats.NetUploadSpeed) / (1024.0 * 1024.0)
		}
	} else if event.ServerID != "" {
		norm.ServerID = event.ServerID
		norm.CPUPercent = event.CPU
		if event.BytesIn > 0 {
			norm.DownloadRateMB = float64(event.BytesIn) / (1024.0 * 1024.0)
		}
		if event.BytesOut > 0 {
			norm.UploadRateMB = float64(event.BytesOut) / (1024.0 * 1024.0)
		}
	}

	select {
	case wp.tasks <- norm:
		return true
	default:
		// Queue saturated: release memory and drop to avoid latency cascade
		wp.pools.PutMetric(norm)
		wp.logger.Warnf("Queue full, dropped metric for server %s", norm.ServerID)
		return false
	}
}

func (wp *WorkerPool) workerLoop(workerID int) {
	defer wp.wg.Done()
	for {
		select {
		case <-wp.ctx.Done():
			return
		case metric, ok := <-wp.tasks:
			if !ok {
				return
			}
			wp.handler(metric)
			// Return to pool after processing
			wp.pools.PutMetric(metric)
		}
	}
}
```

### Zero-Allocation Buffer Reuse with sync.Pool
Use `sync.Pool` to recycle objects:

```go
type MemoryPools struct {
	metricPool sync.Pool
	bytePool   sync.Pool
}

func NewMemoryPools() *MemoryPools {
	return &MemoryPools{
		metricPool: sync.Pool{
			New: func() any {
				return &NormalizedMetric{}
			},
		},
		bytePool: sync.Pool{
			New: func() any {
				b := make([]byte, 4096)
				return &b
			},
		},
	}
}

func (p *MemoryPools) GetMetric() *NormalizedMetric {
	m := p.metricPool.Get().(*NormalizedMetric)
	m.Reset()
	return m
}

func (p *MemoryPools) PutMetric(m *NormalizedMetric) {
	if m == nil {
		return
	}
	p.metricPool.Put(m)
}
```

### EWMA Real-Time Rate Calculations
Exponentially Weighted Moving Average provides smooth rate estimation that adapts quickly to persistent changes while filtering transient noise:

$$S_t = \alpha \cdot Y_t + (1 - \alpha) \cdot S_{t-1}$$

Where:
- $Y_t$ is the instantaneous rate measurement at time $t$
- $S_t$ is the calculated smoothed EWMA rate
- $\alpha \in (0, 1]$ is the discount smoothing factor (default $0.2$)

```go
type EWMACalculator struct {
	alpha float64
	mu    sync.RWMutex
	nodes map[string]*NodeEWMAState
}

type NodeEWMAState struct {
	ServerID        string
	SmoothedInRate  float64
	SmoothedOutRate float64
	LastTimestamp   int64
	Initialized     bool
}

func (c *EWMACalculator) Update(m *NormalizedMetric) *NodeEWMAState {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, exists := c.nodes[m.ServerID]
	if !exists {
		state = &NodeEWMAState{
			ServerID:        m.ServerID,
			SmoothedInRate:  m.DownloadRateMB,
			SmoothedOutRate: m.UploadRateMB,
			LastTimestamp:   m.Timestamp.Unix(),
			Initialized:     true,
		}
		c.nodes[m.ServerID] = state
		return state
	}

	// Apply EWMA update formula
	state.SmoothedInRate = c.alpha*m.DownloadRateMB + (1.0-c.alpha)*state.SmoothedInRate
	state.SmoothedOutRate = c.alpha*m.UploadRateMB + (1.0-c.alpha)*state.SmoothedOutRate
	state.LastTimestamp = m.Timestamp.Unix()
	return state
}
```

### Telemetry Anomaly Detection & Alerts
Evaluate incoming telemetry against statistical baselines:

```go
type AnomalyDetector struct {
	spikeThresholdMB float64
	host             pluginsdk.HostContext
	logger           pluginsdk.Logger
	lastAlertTimes   sync.Map
}

func (d *AnomalyDetector) Evaluate(ctx context.Context, state *NodeEWMAState) {
	inRateMB := state.SmoothedInRate / (1024 * 1024)
	outRateMB := state.SmoothedOutRate / (1024 * 1024)

	if inRateMB > d.spikeThresholdMB || outRateMB > d.spikeThresholdMB {
		// Enforce cooldown (1 alert per server every 5 minutes)
		if val, ok := d.lastAlertTimes.Load(state.ServerID); ok {
			if time.Since(val.(time.Time)) < 5*time.Minute {
				return
			}
		}

		d.lastAlertTimes.Store(state.ServerID, time.Now())
		d.logger.Warnf("Anomaly detected on server %s (In: %.2f MB/s, Out: %.2f MB/s)",
			state.ServerID, inRateMB, outRateMB)

		_ = d.host.SendNotification(ctx, pluginsdk.Notification{
			Title:    "Network Traffic Spike Alert",
			Message:  fmt.Sprintf("Server %s exceeded traffic threshold: In=%.2f MB/s, Out=%.2f MB/s", state.ServerID, inRateMB, outRateMB),
			ServerID: state.ServerID,
			Details: map[string]any{
				"level":       "warning",
				"category":    "network",
				"tags":        []string{"traffic", "spike", state.ServerID},
				"in_rate_mb":  inRateMB,
				"out_rate_mb": outRateMB,
			},
		})
	}
}
```

---

## 5. Frontend Plugin Development

QY-DASH frontend plugins are written as native ECMAScript Modules (`.js`) loaded directly by modern browsers via dynamic `import()`.

### window.__QY_PLUGIN_SDK__ API Reference
The host dashboard exposes the `QYPluginSDK` interface globally:

```typescript
interface QYPluginSDK {
  version: string;
  
  // React 19 Runtime
  React: typeof React;
  ReactDOM: typeof ReactDOM;

  // Curated Lucide Icons
  icons: {
    Activity: React.ComponentType<{ className?: string }>;
    LineChart: React.ComponentType<{ className?: string }>;
    Server: React.ComponentType<{ className?: string }>;
    HardDrive: React.ComponentType<{ className?: string }>;
    AlertTriangle: React.ComponentType<{ className?: string }>;
    RefreshCw: React.ComponentType<{ className?: string }>;
    [key: string]: React.ComponentType<{ className?: string }>;
  };

  // Reusable UI Primitives
  components: {
    Button: React.ComponentType<{
      variant?: 'primary' | 'secondary' | 'danger' | 'ghost';
      size?: 'sm' | 'md' | 'lg';
      onClick?: () => void;
      children?: React.ReactNode;
      disabled?: boolean;
      className?: string;
    }>;
    Badge: React.ComponentType<{
      variant?: 'success' | 'warning' | 'danger' | 'info';
      children?: React.ReactNode;
      className?: string;
    }>;
    Card: React.ComponentType<{
      title?: string;
      children?: React.ReactNode;
      className?: string;
    }>;
    Modal: React.ComponentType<{
      isOpen: boolean;
      onClose: () => void;
      title?: string;
      children?: React.ReactNode;
    }>;
  };

  // Authenticated Host API Client (automatically attaches CSRF/JWT tokens)
  api: {
    get: <T = any>(endpoint: string) => Promise<T>;
    post: <T = any>(endpoint: string, body?: any) => Promise<T>;
    delete: <T = any>(endpoint: string) => Promise<T>;
  };

  // Slot Extension Registry
  registry: {
    registerTab: (tab: PluginTabDefinition) => () => void;
    registerDrawerAction: (action: PluginDrawerActionDefinition) => () => void;
    registerWidget: (widget: PluginWidgetDefinition) => () => void;
  };
}
```

### Extension Points & UI Slots

#### 1. Navigation Tab (`registerTab`)
Adds top-level tab in the main navigation header:

```javascript
const unregisterTab = sdk.registry.registerTab({
  id: 'metrics-analysis-tab',
  pluginId: 'metrics-analyzer',
  title: 'Metrics Analysis',
  icon: 'Activity',
  component: () => {
    const { useState, useEffect } = sdk.React;
    const { Card, Badge, Button } = sdk.components;
    const [data, setData] = useState(null);

    useEffect(() => {
      sdk.api.get('/api/v1/plugins/metrics-analyzer/storage?prefix=ewma')
        .then(items => setData(items))
        .catch(console.error);
    }, []);

    return sdk.React.createElement(Card, { title: 'Probe Telemetry Analyzer' },
      sdk.React.createElement('div', { className: 'p-4 text-sm text-gray-300' },
        'Active analysis sessions: ',
        sdk.React.createElement(Badge, { variant: 'success' }, data ? data.length : 0)
      )
    );
  }
});
```

#### 2. Server Drawer Action (`registerDrawerAction`)
Adds action buttons to server inspection drawers:

```javascript
const unregisterAction = sdk.registry.registerDrawerAction({
  id: 'server-inspect-ewma',
  pluginId: 'metrics-analyzer',
  title: 'Inspect EWMA Rates',
  icon: 'LineChart',
  onClick: (server) => {
    console.log('Inspecting server:', server.id, server.name);
    // Display modal or load drilldown telemetry
  }
});
```

#### 3. Overview Widget (`registerWidget`)
Injects summary cards into the main overview stats grid:

```javascript
const unregisterWidget = sdk.registry.registerWidget({
  id: 'ewma-traffic-widget',
  pluginId: 'metrics-analyzer',
  title: 'EWMA Traffic Throughput',
  span: 2, // 1 to 4 column span on 4-column responsive grid
  component: () => {
    const { Card, Badge } = sdk.components;
    return sdk.React.createElement(Card, { title: 'Real-Time EWMA Aggregation' },
      sdk.React.createElement('div', { className: 'flex justify-between items-center py-2' },
        sdk.React.createElement('span', { className: 'text-xs text-gray-400' }, 'Cluster Ingress'),
        sdk.React.createElement(Badge, { variant: 'info' }, '48.2 MB/s')
      )
    );
  }
});
```

### React 19 Compatibility & Error Boundaries
- **No Direct Bundling**: Do not bundle `react` or `react-dom` inside your plugin build. Extract them as externals using your build tool (`esbuild` or `vite`).
- **Hook Isolation**: Always access React hooks from `sdk.React` (`sdk.React.useState`, `sdk.React.useEffect`, `sdk.React.useMemo`).
- **Error Boundaries**: The host wraps all extension slots in React Error Boundaries. If a plugin component throws, the host isolates the failure and displays a fallback notice without crashing the surrounding dashboard.

### Hot-Unmount & Cleanup Protocols
When a plugin is disabled or uninstalled, the host calls its `deactivate()` export. Always return teardown functions and clear timers, subscriptions, or intervals:

```javascript
let cleanupFns = [];

export function activate(sdk) {
  // Register extension slots and collect unregister callbacks
  cleanupFns.push(sdk.registry.registerTab(myTab));
  cleanupFns.push(sdk.registry.registerWidget(myWidget));

  const intervalId = setInterval(() => {
    // Background polling if needed
  }, 5000);
  cleanupFns.push(() => clearInterval(intervalId));
}

export function deactivate() {
  // Execute all cleanups: unmounts UI slots from DOM and frees listeners
  cleanupFns.forEach(fn => fn());
  cleanupFns = [];
}
```

---

## 6. Complete End-to-End Tutorial: Reference Plugin

Let's inspect the official `metrics-analyzer` reference plugin (`plugins/metrics-analyzer`) demonstrating all features.

### Project Structure
```
plugins/metrics-analyzer/
├── plugin.json               # Plugin Manifest
├── main.go                   # Go Entrypoint calling pluginsdk.Serve()
├── analyzer/                 # Go Telemetry Engine
│   ├── analyzer.go           # pluginsdk.Plugin implementation
│   ├── worker_pool.go        # Concurrent worker pool
│   ├── pool.go               # sync.Pool memory recyclers
│   ├── ewma.go               # Mathematical EWMA calculator
│   ├── anomaly.go            # Spike anomaly detector & notification dispatcher
│   ├── storage.go            # Periodic storage persistence flusher
│   └── types.go              # Data structures & configs
└── ui/                       # Frontend Native ESM bundle
    └── index.js              # ESM exporting activate(sdk) & deactivate()
```

### Backend Implementation

#### `main.go`
```go
package main

import (
	"log"

	"qy-dash/pkg/pluginsdk"
	"qy-dash/plugins/metrics-analyzer/analyzer"
)

func main() {
	cfg := analyzer.DefaultConfig()
	plugin := analyzer.New(cfg)

	// pluginsdk.Serve handles UDS connection, handshake, heartbeat,
	// and invokes plugin.Start(hostContext) and plugin.Stop() on shutdown.
	if err := pluginsdk.Serve(plugin); err != nil {
		log.Fatalf("[metrics-analyzer] Fatal runtime error: %v", err)
	}
}
```

#### `analyzer/analyzer.go`
```go
package analyzer

import (
	"context"
	"fmt"
	"sync"

	"qy-dash/pkg/pluginsdk"
)

type MetricsAnalyzerPlugin struct {
	cfg        Config
	host       pluginsdk.HostContext
	pools      *MemoryPools
	calc       *EWMACalculator
	workerPool *WorkerPool
	detector   *AnomalyDetector
	flusher    *StorageFlusher
	sub        pluginsdk.MetricsSubscription
	logger     pluginsdk.Logger

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
}

func New(cfg Config) *MetricsAnalyzerPlugin {
	return &MetricsAnalyzerPlugin{
		cfg:   cfg,
		pools: NewMemoryPools(),
		calc:  NewEWMACalculator(cfg.EWMAAlpha),
	}
}

func (p *MetricsAnalyzerPlugin) Start(host pluginsdk.HostContext) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.host = host
	p.logger = host.Log()
	p.ctx, p.cancel = context.WithCancel(host.Context())

	p.logger.Infof("Initializing Metrics Analyzer Plugin (Workers=%d, Alpha=%.2f)",
		p.cfg.NumWorkers, p.cfg.EWMAAlpha)

	p.detector = NewAnomalyDetector(p.cfg, host, p.logger)
	p.flusher = NewStorageFlusher(p.cfg, host, p.calc, p.pools, p.logger)

	handler := func(metric *NormalizedMetric) {
		state := p.calc.Update(metric)
		p.detector.Evaluate(p.ctx, state)
	}

	p.workerPool = NewWorkerPool(p.cfg, p.pools, handler, p.logger)

	subOpts := pluginsdk.SubscribeOptions{
		ServerIDs:  nil, // Subscribe to all servers
		BufferSize: p.cfg.QueueCapacity,
	}

	sub, err := host.SubscribeMetrics(p.ctx, subOpts, pluginsdk.MetricsHandlerFunc(func(event *pluginsdk.MetricsEvent) {
		p.workerPool.Ingest(event, nil)
	}))
	if err != nil {
		return fmt.Errorf("subscribe metrics: %w", err)
	}
	p.sub = sub

	p.logger.Infof("Metrics Analyzer Plugin successfully subscribed to telemetry stream")
	return nil
}

func (p *MetricsAnalyzerPlugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.logger.Infof("Stopping Metrics Analyzer Plugin...")
	if p.cancel != nil {
		p.cancel()
	}
	if p.sub != nil {
		_ = p.sub.Close()
	}
	if p.workerPool != nil {
		p.workerPool.Stop()
	}
	if p.flusher != nil {
		p.flusher.Stop()
	}
	p.logger.Infof("Metrics Analyzer Plugin stopped cleanly")
	return nil
}
```

### Frontend ESM Module (`ui/index.js`)
```javascript
let unregisterCallbacks = [];

export function activate(sdk) {
  const React = sdk.React;
  const { Card, Badge, Button } = sdk.components;

  // 1. Register Overview Widget
  const unregisterWidget = sdk.registry.registerWidget({
    id: 'ewma-traffic-widget',
    pluginId: 'metrics-analyzer',
    title: 'EWMA Traffic Rates',
    span: 2,
    component: () => {
      const [rates, setRates] = React.useState({ inMB: 0, outMB: 0 });

      React.useEffect(() => {
        const fetchRates = () => {
          sdk.api.get('/api/v1/plugins/metrics-analyzer/storage?key=ewma:cluster_snapshot')
            .then(res => {
              if (res && res.value) {
                const parsed = JSON.parse(res.value);
                setRates({
                  inMB: (parsed.smoothed_in_rate / (1024 * 1024)).toFixed(2),
                  outMB: (parsed.smoothed_out_rate / (1024 * 1024)).toFixed(2)
                });
              }
            })
            .catch(() => {});
        };

        fetchRates();
        const id = setInterval(fetchRates, 3000);
        return () => clearInterval(id);
      }, []);

      return React.createElement(Card, { title: 'EWMA Traffic Telemetry' },
        React.createElement('div', { className: 'flex justify-between items-center py-1' },
          React.createElement('span', { className: 'text-sm text-gray-400' }, 'Cluster In:'),
          React.createElement(Badge, { variant: 'info' }, rates.inMB + ' MB/s')
        ),
        React.createElement('div', { className: 'flex justify-between items-center py-1' },
          React.createElement('span', { className: 'text-sm text-gray-400' }, 'Cluster Out:'),
          React.createElement(Badge, { variant: 'success' }, rates.outMB + ' MB/s')
        )
      );
    }
  });
  unregisterCallbacks.push(unregisterWidget);
}

export function deactivate() {
  unregisterCallbacks.forEach(fn => fn());
  unregisterCallbacks = [];
}
```

### Compiling, Packaging & Verifying

#### 1. Cross-Compile Go Subprocess Binary
```bash
# Linux amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/metrics-analyzer-linux-amd64 main.go

# Linux arm64
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o bin/metrics-analyzer-linux-arm64 main.go

# Darwin arm64 (Apple Silicon)
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o bin/metrics-analyzer-darwin-arm64 main.go
```

#### 2. Create Distribution Zip
```bash
# Package into release zip
zip -r metrics-analyzer-v1.0.0.zip plugin.json bin/ ui/

# Calculate SHA-256 for marketplace publishing
sha256sum metrics-analyzer-v1.0.0.zip
```

#### 3. Test & Verify Locally
Deploy in development symlink mode:
```bash
ln -s $(pwd)/plugins/metrics-analyzer /var/lib/qy-dash/plugins/metrics-analyzer
```
Toggle the plugin in QY-DASH Settings -> Plugins or via REST API:
```bash
curl -X POST http://localhost:8080/api/v1/plugins/metrics-analyzer/enable \
  -H "Authorization: Bearer $TOKEN"
```
Verify the plugin's status, live metrics streaming, and the new UI widget mounted in your browser.
