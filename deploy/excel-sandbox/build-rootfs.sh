#!/usr/bin/env bash
# Build the excel-sandbox rootfs tarball.
#
# Run this ONCE on any machine with Docker (dev box / CI). The output
# excel-sandbox-rootfs.tar.xz is a release artifact — copy it to the gateway
# host and extract next to the nsjail binary. The production host never
# touches Docker (design doc §5.6.3).
#
# Usage: deploy/excel-sandbox/build-rootfs.sh [output-dir]
set -euo pipefail

cd "$(dirname "$0")"
OUT_DIR="${1:-.}"
IMAGE="excel-sandbox-rootfs:local"
TAR="$OUT_DIR/excel-sandbox-rootfs.tar"
TARXZ="$OUT_DIR/excel-sandbox-rootfs.tar.xz"

echo "==> docker build $IMAGE"
docker build -t "$IMAGE" .

echo "==> docker export"
cid=$(docker create "$IMAGE")
trap 'docker rm -f "$cid" >/dev/null 2>&1 || true' EXIT
docker export "$cid" -o "$TAR"
docker rm "$cid" >/dev/null
trap - EXIT

echo "==> xz compress (this is the slow part, ~80-150MB compressed result)"
xz -T0 -6 -f "$TAR"

echo "==> sha256"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$TARXZ"
else
  shasum -a 256 "$TARXZ"
fi

echo "==> done: $TARXZ"
cat <<'EOF'

On the gateway host (as root, in the gateway data directory):
  mkdir -p sandbox/rootfs
  tar -xJf excel-sandbox-rootfs.tar.xz -C sandbox/rootfs
  # place the static nsjail binary at sandbox/nsjail (see README.md)
  # verify:  sandbox/nsjail -Mo --chroot sandbox/rootfs --user 65534 \
             --group 65534 --cwd /tmp --tmpfsmount /tmp:size=32m \
             -- /usr/local/bin/python3 -I - <<< 'print(1+1)'
EOF
