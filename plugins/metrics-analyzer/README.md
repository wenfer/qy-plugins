# Metrics Analyzer Plugin

Official reference plugin for QY-DASH demonstrating high-throughput real-time probe telemetry ingestion, zero-allocation memory recycling via `sync.Pool`, Exponentially Weighted Moving Average (EWMA) traffic smoothing, and automatic spike anomaly alerts.

## Features

- **Microkernel Subprocess**: Runs in a sandboxed child process with strict resource limits and sub-10ms crash detection.
- **IPC Protocol**: 16-byte binary framing protocol over Unix Domain Sockets (`/tmp/qysock-*`).
- **Telemetry Streaming**: Subscribes to real-time probe metrics without blocking host operation.
- **EWMA Traffic Rate**: Computes smoothed traffic rate ($\alpha = 0.20$) and flags abnormal traffic spikes (>50 MB/s).
- **Isolated SQLite Storage**: Persists rolling metrics snapshots via host-managed isolated KV tables.
- **Native ESM UI**: Dynamic React frontend extension points (Navigation Tab, Server Drawer Action, Overview Widget).
