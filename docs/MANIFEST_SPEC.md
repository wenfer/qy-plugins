# QY-DASH Plugin Manifest & Distribution Specification

Official specification for `plugin.json` manifest structure, multi-architecture packaging, security permissions, GitHub Marketplace catalog publishing (`index.json`), and installation vectors.

---

## Table of Contents
1. [plugin.json Manifest Specification](#1-pluginjson-manifest-specification)
   - [Complete Schema Reference](#complete-schema-reference)
   - [Root Metadata Fields](#root-metadata-fields)
   - [Backend Execution Block (`main` / `entrypoint`)](#backend-execution-block-main--entrypoint)
   - [Frontend Dynamic Loading Block (`frontend`)](#frontend-dynamic-loading-block-frontend)
   - [Permissions & Capabilities Declarations](#permissions--capabilities-declarations)
2. [Multi-Architecture Compilation & Packaging](#2-multi-architecture-compilation--packaging)
   - [Directory Structure](#directory-structure)
   - [Cross-Compiling Subprocess Binaries](#cross-compiling-subprocess-binaries)
   - [Archive Creation & File Permissions (0755 / 0644)](#archive-creation--file-permissions-0755--0644)
   - [SHA-256 Checksum Calculation](#sha-256-checksum-calculation)
3. [GitHub Marketplace Catalog (`index.json`)](#3-github-marketplace-catalog-indexjson)
   - [Catalog Schema Version 1](#catalog-schema-version-1)
   - [Official Catalog Repository: `wenfer/qy-plugins`](#official-catalog-repository-qy-pluginsindex)
   - [Mirror Acceleration Engine (`ghproxy.net` & Fallbacks)](#mirror-acceleration-engine-ghproxynet--fallbacks)
4. [Installation Vectors & Lifecycle Workflows](#4-installation-vectors--lifecycle-workflows)
   - [Vector 1: One-Click Marketplace Installation](#vector-1-one-click-marketplace-installation)
   - [Vector 2: GitHub Repository Direct Installation](#vector-2-github-repository-direct-installation)
   - [Vector 3: Offline Zip Archive Upload](#vector-3-offline-zip-archive-upload)
   - [Vector 4: Local Development Symlink Mode](#vector-4-local-development-symlink-mode)
5. [Security & Validation Rules](#5-security--validation-rules)
   - [Zip-Slip & Path Traversal Mitigations](#zip-slip--path-traversal-mitigations)
   - [Decompression Bomb Cumulative Limits](#decompression-bomb-cumulative-limits)
   - [Reserved Identifiers & Name Restrictions](#reserved-identifiers--name-restrictions)

---

## 1. plugin.json Manifest Specification

The `plugin.json` file resides in the root directory of the plugin archive. It describes plugin metadata, execution parameters, frontend extension points, and required security permissions.

### Complete Schema Reference
```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "id": "metrics-analyzer",
  "name": "Metrics Analyzer",
  "version": "1.0.0",
  "author": "QY-DASH Core Team",
  "description": "Real-time traffic analyzer with worker pool, buffer recycling, EWMA rate computation, and anomaly alert dispatching.",
  "homepage": "https://github.com/wenfer/qy-plugins",
  "repository": "https://github.com/wenfer/qy-plugins",
  "license": "MIT",
  "min_host_version": "2.0.0",
  "main": "bin/metrics-analyzer",
  "entrypoint": {
    "binary": "bin/metrics-analyzer",
    "args": [
      "--buffer-size=1024",
      "--workers=8"
    ],
    "env": {
      "ANALYZER_EWMA_ALPHA": "0.2",
      "ANALYZER_SPIKE_THRESHOLD_MB": "50.0"
    }
  },
  "frontend": {
    "esm": "ui/index.js",
    "extension_points": {
      "navigation_tabs": [
        {
          "id": "metrics-analyzer-tab",
          "title": "Metrics Analysis",
          "icon": "Activity",
          "component": "MetricsTab"
        }
      ],
      "drawer_actions": [
        {
          "id": "server-ewma-stats",
          "title": "EWMA Rate Inspector",
          "icon": "LineChart",
          "component": "ServerEWMAAction"
        }
      ],
      "overview_widgets": [
        {
          "id": "ewma-traffic-widget",
          "title": "EWMA Traffic Rates",
          "span": 2,
          "component": "TrafficWidget"
        }
      ]
    }
  },
  "permissions": [
    "metrics:subscribe",
    "storage:isolated",
    "storage:readwrite",
    "servers:read",
    "notifications:send"
  ],
  "capabilities": [
    "metrics_streaming",
    "ewma_aggregation",
    "anomaly_detection",
    "ui_extension"
  ]
}
```

### Root Metadata Fields

| Field | Type | Required | Description |
|---|---|:---:|---|
| `id` | `string` | **Yes** | Unique identifier matching regex `^[a-zA-Z0-9_-]+$`. Cannot be `"install"` or `"market"` (reserved). |
| `name` | `string` | **Yes** | Human-readable plugin display name. |
| `version` | `string` | **Yes** | Semantic version string (e.g., `1.0.0`, `2.1.4`). |
| `description` | `string` | No | Brief explanation of plugin functionality and features. |
| `author` | `string` | No | Author name or development organization. |
| `homepage` | `string` | No | Web URL to documentation or project website. |
| `repository` | `string` | No | URL to Git source code repository. |
| `license` | `string` | No | Open source license identifier (e.g., `MIT`, `Apache-2.0`). |
| `min_host_version`| `string` | No | Minimum QY-DASH host version required (e.g., `2.0.0`). |

### Backend Execution Block (`main` / `entrypoint`)

The backend binary must be located within the plugin directory (relative path). Absolute paths or paths with `..` directory traversal are rejected during installation.

- **`main`** (`string`, Required): Relative path to backend executable binary (e.g., `bin/metrics-analyzer`).
- **`entrypoint`** (`object`, Optional):
  - `binary` (`string`): Fallback binary path if `main` is omitted.
  - `args` (`string[]`): Command-line flags passed to the subprocess on launch.
  - `env` (`map[string]string`): Custom environment variables injected into the subprocess.

### Frontend Dynamic Loading Block (`frontend`)

Configures browser dynamic ESM import and UI registration:

- **`esm`** (`string`): Relative path to compiled ESM bundle (e.g., `ui/index.js`).
- **`extension_points`** (`object`):
  - **`navigation_tabs`** (`array`): Navigation entries registered in top header bar.
    - `id` (`string`): Unique tab identifier.
    - `title` (`string`): Header label.
    - `icon` (`string`): Lucide icon identifier (e.g., `Activity`, `HardDrive`, `Server`, `Settings`).
    - `component` (`string`): Exported component name in ESM module.
  - **`drawer_actions`** (`array`): Action buttons added to server detail drawer.
    - `id` (`string`): Action identifier.
    - `title` (`string`): Action button label.
    - `icon` (`string`): Lucide icon name.
    - `component` (`string`): Modal or handler component name.
  - **`overview_widgets`** (`array`): Dashboard statistic cards added to Overview page.
    - `id` (`string`): Widget identifier.
    - `title` (`string`): Widget card title.
    - `span` (`number`): Grid column width (1 to 4 on a 4-column responsive grid).
    - `component` (`string`): Widget component name.

### Permissions & Capabilities Declarations

Explicit declaration of host services required by the plugin:

#### Permissions (`permissions`)
- `metrics:subscribe`: Authorizes real-time probe metrics stream subscription.
- `storage:isolated`: Grants read/write access to isolated SQLite Key-Value table.
- `storage:readwrite`: Unrestricted storage operations within plugin keyspace.
- `servers:read`: Allows querying host server topology and node details.
- `notifications:send`: Authorizes dispatching administrative alert notifications.

#### Capabilities (`capabilities`)
Informational tags consumed by marketplace search index and UI filters:
- `metrics_streaming`: Continuous metrics aggregation.
- `ewma_aggregation`: High-performance mathematical statistical rate modeling.
- `anomaly_detection`: Real-time anomaly detection and threshold evaluation.
- `ui_extension`: Provides interactive web frontend UI components.

---

## 2. Multi-Architecture Compilation & Packaging

### Directory Structure
```
my-plugin-archive/
├── plugin.json                 # Required: Root manifest
├── bin/
│   ├── my-plugin               # Active executable (or architecture-specific binary)
│   ├── my-plugin-linux-amd64   # Pre-built target
│   ├── my-plugin-linux-arm64   # Pre-built target
│   └── my-plugin-darwin-arm64  # Pre-built target
├── ui/
│   └── index.js                # Frontend Native ESM bundle
└── README.md                   # Documentation
```

### Cross-Compiling Subprocess Binaries
Compile pure Go binaries using `CGO_ENABLED=0` and linker flags to strip debugging symbols (`-ldflags="-s -w"`):

```bash
# 1. Linux x86_64 (amd64)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/my-plugin-linux-amd64 main.go

# 2. Linux ARM64 (aarch64)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o bin/my-plugin-linux-arm64 main.go

# 3. Darwin ARM64 (macOS Apple Silicon)
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o bin/my-plugin-darwin-arm64 main.go
```

For single-target deployment packages, symlink or rename the appropriate target to `bin/my-plugin` matching `main` in `plugin.json`.

### Archive Creation & File Permissions (0755 / 0644)
During extraction, QY-DASH automatically enforces Unix file permissions:
- Binaries (`main` and any file in `bin/`): `0755` (`-rwxr-xr-x`).
- Regular files (`plugin.json`, `ui/*.js`, configs): `0644` (`-rw-r--r--`).
- Directories: `0755` (`drwxr-xr-x`).

Create distribution archive:
```bash
zip -r my-plugin-v1.0.0.zip plugin.json bin/ ui/ README.md
```

### SHA-256 Checksum Calculation
Every release published to the marketplace or downloaded via URL requires an SHA-256 checksum to guarantee file integrity:

```bash
# Linux
sha256sum my-plugin-v1.0.0.zip

# macOS
shasum -a 256 my-plugin-v1.0.0.zip
```
Example output:
`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`

---

## 3. GitHub Marketplace Catalog (`index.json`)

Official catalog index is hosted at `https://raw.githubusercontent.com/wenfer/qy-plugins/main/index.json`.

### Catalog Schema Version 1
```json
{
  "version": 1,
  "updated_at": "2026-11-14T00:00:00Z",
  "plugins": [
    {
      "id": "metrics-analyzer",
      "name": "Metrics Analyzer",
      "version": "1.0.0",
      "description": "Real-time traffic analyzer with worker pool, buffer recycling, EWMA rate computation, and anomaly alert dispatching.",
      "author": "QY-DASH Core Team",
      "homepage": "https://github.com/wenfer/qy-plugins",
      "repository": "https://github.com/wenfer/qy-plugins",
      "license": "MIT",
      "tags": ["monitoring", "metrics", "ewma", "analytics"],
      "min_host_version": "2.0.0",
      "releases": [
        {
          "version": "1.0.0",
          "target_os": "linux",
          "target_arch": "amd64",
          "download_url": "https://github.com/wenfer/qy-plugins/releases/download/v1.0.0/metrics-analyzer-linux-amd64.zip",
          "sha256": "4a5e3b6189b21f37ccb839a2d1283948ec78913210abced45678912345678901",
          "size_bytes": 12845920,
          "published_at": "2026-11-14T00:00:00Z"
        },
        {
          "version": "1.0.0",
          "target_os": "linux",
          "target_arch": "arm64",
          "download_url": "https://github.com/wenfer/qy-plugins/releases/download/v1.0.0/metrics-analyzer-linux-arm64.zip",
          "sha256": "8f9a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a",
          "size_bytes": 11984210,
          "published_at": "2026-11-14T00:00:00Z"
        },
        {
          "version": "1.0.0",
          "target_os": "darwin",
          "target_arch": "arm64",
          "download_url": "https://github.com/wenfer/qy-plugins/releases/download/v1.0.0/metrics-analyzer-darwin-arm64.zip",
          "sha256": "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b",
          "size_bytes": 12345678,
          "published_at": "2026-11-14T00:00:00Z"
        }
      ]
    }
  ]
}
```

### Official Catalog Repository: `wenfer/qy-plugins`
To list a new plugin in the official market:
1. Create a release on your GitHub repository attaching cross-compiled `.zip` files.
2. Submit a Pull Request to `wenfer/qy-plugins` adding your entry to `index.json`.
3. Provide valid SHA-256 hashes generated from the release assets.

### Mirror Acceleration Engine (`ghproxy.net` & Fallbacks)
To accelerate downloads in network environments with restricted GitHub access (such as mainland China), QY-DASH integrates automatic mirror rewriting.

- **Configurable Mirror**: Default is `https://ghproxy.net/`, or any custom reverse proxy endpoint configured in Settings.
- **URL Rewriting Rules**:
  - `https://github.com/owner/repo/releases/download/...` $\rightarrow$ `https://ghproxy.net/https://github.com/owner/repo/releases/download/...`
  - Relative release paths $\rightarrow$ resolved and prepended with mirror.
- **Automatic Fallback**: If downloading through the mirror encounters HTTP errors (4xx, 5xx) or network timeouts, the client logs a warning and automatically falls back to direct GitHub download.
- **Strict Verification**: Regardless of whether the package arrived via mirror or direct URL, SHA-256 verification is performed prior to disk extraction.

---

## 4. Installation Vectors & Lifecycle Workflows

QY-DASH supports 4 flexible installation vectors:

```
                                  +------------------------------+
                                  |   Plugin Installation Hub   |
                                  +------------------------------+
                                                  |
           +----------------------+---------------+----------------------+
           |                      |               |                      |
           v                      v               v                      v
    [ Vector 1 ]           [ Vector 2 ]     [ Vector 3 ]           [ Vector 4 ]
    Market 1-Click         GitHub Git URL   Offline Zip Upload     Dev Symlink
           |                      |               |                      |
    (Catalog Sync)         (Release Fetch)  (Multipart Post)       (Local Link)
           |                      |               |                      |
           v                      v               v                      |
    +--------------+       +--------------+ +--------------+             |
    | Mirror Accel |       | Mirror Accel | | Temp Staging |             |
    +--------------+       +--------------+ +--------------+             |
           |                      |               |                      |
           v                      v               v                      |
    +---------------------------------------------+                      |
    |          SHA-256 Checksum Validation        |                      |
    +---------------------------------------------+                      |
                           |                                             |
                           v                                             v
    +---------------------------------------------+        +--------------------+
    |      Two-Pass Safe Decompression (Unpack)   |        | Direct Disk Link   |
    |  - Pass 1: Zip-Slip & Path Escape Checks    |        | (Instant Iteration)|
    |  - Pass 2: Cumulative Bomb Limits (200MB)   |        |                    |
    |  - Pass 2: Permission Enforcement (0755)    |        +--------------------+
    +---------------------------------------------+                  |
                           |                                         |
                           +--------------------+--------------------+
                                                |
                                                v
                                  +---------------------------+
                                  | SQLite plugins Metadata   |
                                  | Status: INSTALLED         |
                                  +---------------------------+
                                                |
                                                v
                                  +---------------------------+
                                  | Supervisor Enable / Start |
                                  | UDS Handshake & Subprocess|
                                  +---------------------------+
```

### Vector 1: One-Click Marketplace Installation
Installs catalog plugins directly from the web UI via the unified `/api/v1/plugins/install` endpoint:

```http
POST /api/v1/plugins/install HTTP/1.1
Content-Type: application/json
Authorization: Bearer <TOKEN>

{
  "source_type": "market",
  "plugin_id": "metrics-analyzer",
  "version": "1.0.0",
  "enable": true
}
```

**Lifecycle**:
1. Host queries `catalog.json` for target OS (`linux` or `darwin`) and architecture (`amd64` or `arm64`).
2. Applies mirror URL rewriting (`ghproxy.net`).
3. Streams release zip directly to temporary staging file.
4. Performs SHA-256 checksum verification against catalog value.
5. Decompresses into `/var/lib/qy-dash/plugins/<id>/`.
6. Enforces `0755` executable permissions on binary.
7. Inserts row into `plugins` table (`status = installed`, `install_type = marketplace`).
8. If `enable: true` is requested, automatically enables and boots the plugin via supervisor.

### Vector 2: GitHub Repository Direct Installation
Install any public GitHub release archive:

```http
POST /api/v1/plugins/install HTTP/1.1
Content-Type: application/json
Authorization: Bearer <TOKEN>

{
  "source_type": "url",
  "url": "https://github.com/my-org/custom-plugin/releases/download/v1.2.0/custom-plugin.zip",
  "version": "1.2.0",
  "sha256": "a3f5b8c9d0e1...",
  "enable": true
}
```

### Vector 3: Offline Zip Archive Upload
Upload offline or air-gapped plugin zip files through multipart form data to `/api/v1/plugins/install`:

```http
POST /api/v1/plugins/install HTTP/1.1
Content-Type: multipart/form-data; boundary=----WebKitFormBoundaryXYZ
Authorization: Bearer <TOKEN>

------WebKitFormBoundaryXYZ
Content-Disposition: form-data; name="file"; filename="my-plugin.zip"
Content-Type: application/zip

<binary zip contents...>
------WebKitFormBoundaryXYZ
Content-Disposition: form-data; name="enable"

true
------WebKitFormBoundaryXYZ
Content-Disposition: form-data; name="sha256"

a3f5b8c9d0e1...
------WebKitFormBoundaryXYZ--
```

### Vector 4: Local Development Symlink Mode
Ideal for iterative development and local debugging without rebuilding zip archives:

```http
POST /api/v1/plugins/install HTTP/1.1
Content-Type: application/json
Authorization: Bearer <TOKEN>

{
  "source_type": "symlink",
  "source_path": "/Users/admin/vscode/qy-xray/plugins/metrics-analyzer",
  "enable": true
}
```

**Features**:
- Symlinks `/var/lib/qy-dash/plugins/<id>` directly to your development source folder.
- Edits to frontend files (`ui/index.js`) reflect immediately upon browser refresh.
- Recompiling backend binary and toggling Disable/Enable restarts the new binary in milliseconds.
- Safe Uninstallation: Removing a symlinked plugin only deletes the symbolic link; your source code directory remains untouched.

---

## 5. Security & Validation Rules

### Zip-Slip & Path Traversal Mitigations
All zip extraction employs a strict two-pass inspection engine (`internal/plugin/installer/unpacker.go`):

1. **Absolute Path Rejection**: Entries starting with `/` or `\` are rejected immediately with `ErrSecurityViolation`.
2. **Drive Letter Rejection**: Windows drive specifiers (`C:\`, `D:\`) are rejected.
3. **`..` Traversal Block**: Any entry path segment matching `..` triggers immediate rejection.
4. **Boundary Containment**: `filepath.Clean(filepath.Join(destDir, name))` must have clean `destDir` as prefix.
5. **Symlink Escape Protection**: Symlink targets inside archives are inspected; symlinks targeting absolute paths or resolving outside the destination directory trigger `ErrSecurityViolation`.

### Decompression Bomb Cumulative Limits
To protect host disks and memory against "zip bomb" amplification attacks:

| Resource Limit | Default Value | Enforcement |
|---|---|---|
| `MaxFiles` | 1,000 files | Pass 1 checks archive file count |
| `MaxFileSize` | 50 MB | Pass 1 checks declared size; Pass 2 limits stream reader |
| `MaxTotalBytes` | 200 MB | Cumulative extracted byte counter across all entries |

If cumulative decompressed bytes exceed `MaxTotalBytes` or any individual file exceeds `MaxFileSize`, extraction terminates instantly, the partial output is completely purged, and `ErrSecurityViolation` is returned.

### Reserved Identifiers & Name Restrictions
- Plugin IDs must strictly match `^[a-zA-Z0-9_-]+$`.
- The IDs `"install"` and `"market"` are reserved for REST API management endpoints and cannot be registered by plugins.
