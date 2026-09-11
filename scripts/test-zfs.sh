#!/bin/sh
# Creates and destroys only a new file-backed test pool, never an existing pool.
set -eu
command -v zpool >/dev/null
command -v zfs >/dev/null
work=$(mktemp -d)
PVE_SNAPSHOT_TEST_POOL="psatest$(date +%s)$$"
export PVE_SNAPSHOT_TEST_POOL
created=false
cleanup() {
    if [ "$created" = true ]; then zpool destroy "$PVE_SNAPSHOT_TEST_POOL"; fi
    rm -rf "$work"
}
trap cleanup EXIT INT TERM
truncate -s 1G "$work/vdev"
zpool create -m none "$PVE_SNAPSHOT_TEST_POOL" "$work/vdev"
created=true
go test ./pkg/storage/zfs -run TestRealZFSLifecycle -count=1 -v
