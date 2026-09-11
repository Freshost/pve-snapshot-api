#!/bin/bash
# Run from the repository root on Debian/Ubuntu with the go.mod toolchain.
set -euo pipefail
arch=$(dpkg --print-architecture)
version=$(dpkg-parsechangelog -SVersion)
upstream=${version%-*}
# CI starts with a clean checkout; exclude packaging and generated output.
tar czf "../pve-snapshot-api_${upstream}.orig.tar.gz" --exclude=.git --exclude=debian --exclude=dist --exclude=./pve-snapshot-api --transform "s,^\.,pve-snapshot-api-${upstream}," .
if [ "$arch" = amd64 ]; then
    dpkg-buildpackage -us -uc -d
else
    dpkg-buildpackage -us -uc -d -b
fi
mkdir -p dist
cp ../pve-snapshot-api_*.deb ../pve-snapshot-api_*"_${arch}.buildinfo" ../pve-snapshot-api_*"_${arch}.changes" dist/
if [ "$arch" = amd64 ]; then
    cp ../pve-snapshot-api_*.dsc ../pve-snapshot-api_*.orig.tar.gz ../pve-snapshot-api_*.debian.tar.xz dist/
fi
