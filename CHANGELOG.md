# Changelog

## [0.2.0] - 2026-09-10

### Fixed

- Scoped storage permissions, task-token authorization and cluster routing.
- Request-body forwarding, non-ZFS paths and websocket upgrades.
- ZFS clone dependency handling, interrupted copies and retry validation.
- Storage configuration refresh, TLS trust, certificate reload and request limits.

### Added

- Persistent task results and recovery after restart.
- Native ZFS tests and gated amd64/arm64 package releases with checksums.
- Go 1.27.1 toolchain.

### Upgrade notes

Unsupported ZFS names and cross-storage/cross-target-node copies now fail
explicitly. Old task history is unavailable; existing untagged targets are not
accepted as retries. Review [compatibility](docs/compatibility.md) for migration,
CA configuration and task-state requirements.

## [0.1.0] - 2026-03-14

- Initial release with ZFS copy/delete, token authentication, cluster forwarding
  and Debian packaging.
