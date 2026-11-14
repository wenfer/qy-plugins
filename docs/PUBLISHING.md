# Publishing Plugins to QY-DASH Marketplace

This guide outlines the process for submitting new plugins to the official QY-DASH marketplace index (`wenfer/qy-plugins`).

---

## Prerequisites

Before submitting your plugin, ensure:
1. **Manifest Compliance**: Your plugin includes a valid `plugin.json` adhering to [MANIFEST_SPEC.md](MANIFEST_SPEC.md).
2. **Crash Resilience**: Your plugin handles graceful shutdown (`Stop()`) and reconnects without leaking sockets or goroutines.
3. **Multi-Architecture**: Binaries are compiled for target architectures (Linux `amd64`, Linux `arm64`, and optionally macOS `darwin-arm64`).
4. **Archive Layout**:
   - Binaries placed in `bin/` with `0755` executable permissions.
   - `plugin.json` at root of the archive.
   - Frontend ESM (if any) placed in `ui/`.

---

## Submission Workflow

### Step 1: Package Your Plugin
Use the official packaging script or Go cross-compilation:
```bash
./scripts/package-plugin.sh plugins/your-plugin your-plugin 1.0.0
```
This produces `.zip` archives in `dist/` and prints the SHA-256 checksums and `releases` JSON block.

### Step 2: Create a GitHub Release
Upload the generated zip archives to your GitHub repository's release page (e.g. `https://github.com/<owner>/<repo>/releases/tag/v1.0.0`).

### Step 3: Update `index.json`
Add your plugin entry to `index.json` in this repository:

```json
{
  "id": "your-plugin",
  "name": "Your Plugin Name",
  "version": "1.0.0",
  "description": "Clear description of plugin capabilities",
  "author": "Your GitHub Username",
  "homepage": "https://github.com/your-username/your-plugin",
  "repository": "https://github.com/your-username/your-plugin",
  "license": "MIT",
  "tags": ["monitoring", "utility"],
  "min_host_version": "2.0.0",
  "releases": [
    {
      "version": "1.0.0",
      "target_os": "linux",
      "target_arch": "amd64",
      "download_url": "https://github.com/your-username/your-plugin/releases/download/v1.0.0/your-plugin-linux-amd64.zip",
      "sha256": "<calculated-sha256-hash>",
      "size_bytes": 1234567,
      "published_at": "2026-11-14T00:00:00Z"
    }
  ]
}
```

### Step 4: Validate and Submit PR
1. Run `./scripts/validate-index.sh` to verify syntax and integrity.
2. Submit a Pull Request to `wenfer/qy-plugins`.
3. CI will automatically run schema validation. Once reviewed and merged, your plugin will instantly appear in all QY-DASH consoles worldwide!
