#!/usr/bin/env bash
set -euo pipefail

# QY-DASH Plugin Packaging Utility
# Cross-compiles Go plugins and creates specification-compliant ZIP packages with SHA-256 hashes.

PLUGIN_DIR="${1:-plugins/metrics-analyzer}"
PLUGIN_ID="${2:-metrics-analyzer}"
VERSION="${3:-1.0.0}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
STAGE_DIR="${ROOT_DIR}/.build_staging"

if [ ! -d "${ROOT_DIR}/${PLUGIN_DIR}" ]; then
  echo "Error: Directory ${ROOT_DIR}/${PLUGIN_DIR} does not exist" >&2
  exit 1
fi

MANIFEST="${ROOT_DIR}/${PLUGIN_DIR}/plugin.json"
if [ ! -f "${MANIFEST}" ]; then
  echo "Error: plugin.json not found in ${PLUGIN_DIR}" >&2
  exit 1
fi

mkdir -p "${DIST_DIR}"
rm -rf "${STAGE_DIR}"
mkdir -p "${STAGE_DIR}"

TARGETS=(
  "linux amd64"
  "linux arm64"
  "darwin arm64"
)

echo "=========================================================="
echo " Packaging Plugin: ${PLUGIN_ID} v${VERSION}"
echo " Source Directory: ${PLUGIN_DIR}"
echo " Output Directory: ${DIST_DIR}"
echo "=========================================================="

# Check for sha256 command
compute_sha256() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${file}" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${file}" | awk '{print $1}'
  else
    python3 -c "import hashlib, sys; print(hashlib.sha256(open(sys.argv[1], 'rb').read()).hexdigest())" "${file}"
  fi
}

get_file_size() {
  local file="$1"
  if [ "$(uname)" = "Darwin" ]; then
    stat -f%z "${file}"
  else
    stat -c%s "${file}"
  fi
}

echo "Generating packages..."
JSON_ENTRIES=()

for TARGET in "${TARGETS[@]}"; do
  OS=$(echo "${TARGET}" | awk '{print $1}')
  ARCH=$(echo "${TARGET}" | awk '{print $2}')
  PKG_NAME="${PLUGIN_ID}-${OS}-${ARCH}"
  TARGET_STAGE="${STAGE_DIR}/${PKG_NAME}"
  
  echo "--> Compiling for ${OS}/${ARCH}..."
  mkdir -p "${TARGET_STAGE}/bin"
  
  # Cross-compile binary package
  (
    cd "${ROOT_DIR}"
    CGO_ENABLED=0 GOOS="${OS}" GOARCH="${ARCH}" go build \
      -trimpath \
      -ldflags="-s -w" \
      -o "${TARGET_STAGE}/bin/${PLUGIN_ID}" \
      "./${PLUGIN_DIR}"
  )
    
  chmod 0755 "${TARGET_STAGE}/bin/${PLUGIN_ID}"
  
  # Copy manifest
  cp "${MANIFEST}" "${TARGET_STAGE}/plugin.json"
  
  # Copy UI if present
  if [ -d "${ROOT_DIR}/${PLUGIN_DIR}/ui" ]; then
    cp -r "${ROOT_DIR}/${PLUGIN_DIR}/ui" "${TARGET_STAGE}/ui"
  fi
  
  # Copy README if present
  if [ -f "${ROOT_DIR}/${PLUGIN_DIR}/README.md" ]; then
    cp "${ROOT_DIR}/${PLUGIN_DIR}/README.md" "${TARGET_STAGE}/"
  fi

  # Zip archive
  ZIP_PATH="${DIST_DIR}/${PKG_NAME}.zip"
  rm -f "${ZIP_PATH}"
  
  (cd "${TARGET_STAGE}" && zip -r -q "${ZIP_PATH}" .)
  
  SHA=$(compute_sha256 "${ZIP_PATH}")
  SIZE=$(get_file_size "${ZIP_PATH}")
  NOW=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  
  echo "    Archive: ${ZIP_PATH}"
  echo "    SHA-256: ${SHA}"
  echo "    Size:    ${SIZE} bytes"

  JSON_ENTRIES+=(
"        {
          \"version\": \"${VERSION}\",
          \"target_os\": \"${OS}\",
          \"target_arch\": \"${ARCH}\",
          \"download_url\": \"https://github.com/wenfer/qy-plugins/releases/download/v${VERSION}/${PKG_NAME}.zip\",
          \"sha256\": \"${SHA}\",
          \"size_bytes\": ${SIZE},
          \"published_at\": \"${NOW}\"
        }"
  )
done

rm -rf "${STAGE_DIR}"

echo ""
echo "=========================================================="
echo " Packaging completed successfully!"
echo " index.json 'releases' snippet:"
echo "=========================================================="
echo "      \"releases\": ["
for i in "${!JSON_ENTRIES[@]}"; do
  if [ $i -lt $((${#JSON_ENTRIES[@]} - 1)) ]; then
    echo "${JSON_ENTRIES[$i]},"
  else
    echo "${JSON_ENTRIES[$i]}"
  fi
done
echo "      ]"
echo "=========================================================="
