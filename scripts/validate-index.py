#!/usr/bin/env python3
"""
validate-index.py - Validates QY-DASH Plugin Marketplace index.json
"""

import sys
import json
import re
import os
import hashlib
from datetime import datetime

INDEX_PATH = sys.argv[1] if len(sys.argv) > 1 else "index.json"

def error(msg):
    print(f"❌ Validation Error: {msg}", file=sys.stderr)
    sys.exit(1)

def info(msg):
    print(f"✅ {msg}")

def main():
    if not os.path.exists(INDEX_PATH):
        error(f"Catalog file not found: {INDEX_PATH}")

    try:
        with open(INDEX_PATH, "r", encoding="utf-8") as f:
            catalog = json.load(f)
    except Exception as e:
        error(f"Invalid JSON syntax in {INDEX_PATH}: {e}")

    # Root fields
    version = catalog.get("version")
    if version != 1:
        error(f"Unsupported schema version: {version}. Expected 1.")

    updated_at = catalog.get("updated_at")
    if not updated_at:
        error("Missing required root field 'updated_at'")

    plugins = catalog.get("plugins")
    if not isinstance(plugins, list):
        error("Root field 'plugins' must be an array")

    plugin_ids = set()

    for idx, plugin in enumerate(plugins):
        pid = plugin.get("id")
        if not pid or not re.match(r"^[a-z0-9]+(-[a-z0-9]+)*$", pid):
            error(f"Plugin #{idx}: Invalid or missing 'id': {pid}. Must be lowercase kebab-case.")
        if pid in plugin_ids:
            error(f"Duplicate plugin id found: {pid}")
        plugin_ids.add(pid)

        for req in ["name", "version", "description"]:
            if not plugin.get(req):
                error(f"Plugin '{pid}': Missing required field '{req}'")

        releases = plugin.get("releases")
        if not isinstance(releases, list) or len(releases) == 0:
            error(f"Plugin '{pid}': Must contain at least one release")

        platforms = set()
        for r_idx, r in enumerate(releases):
            r_ver = r.get("version")
            r_os = r.get("target_os")
            r_arch = r.get("target_arch")
            r_url = r.get("download_url")
            r_sha = r.get("sha256")

            if not all([r_ver, r_os, r_arch, r_url, r_sha]):
                error(f"Plugin '{pid}' release #{r_idx}: Missing required fields in release")

            if not re.match(r"^[a-f0-9]{64}$", r_sha.lower()):
                error(f"Plugin '{pid}' release #{r_idx}: Invalid SHA-256 hash '{r_sha}'")

            plat_key = f"{r_ver}/{r_os}/{r_arch}"
            if plat_key in platforms:
                error(f"Plugin '{pid}': Duplicate release for platform {plat_key}")
            platforms.add(plat_key)

            # If file exists in dist/, check sha256
            pkg_name = f"{pid}-{r_os}-{r_arch}.zip"
            dist_file = os.path.join("dist", pkg_name)
            if os.path.exists(dist_file):
                with open(dist_file, "rb") as bf:
                    actual_sha = hashlib.sha256(bf.read()).hexdigest()
                if actual_sha.lower() != r_sha.lower():
                    error(f"Plugin '{pid}' release {plat_key}: Checksum mismatch for local {dist_file}! Expected {r_sha}, got {actual_sha}")
                print(f"   [Verified] {dist_file} matches declared SHA-256")

    info(f"Successfully validated {INDEX_PATH}: {len(plugins)} plugin(s), schema version {version}.")

if __name__ == "__main__":
    main()
