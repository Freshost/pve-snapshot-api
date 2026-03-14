# pve-snapshot-api

Drop-in Proxmox API middleware that enables **true ZFS snapshots** for [proxmox-csi-plugin](https://github.com/sergelogvinov/proxmox-csi-plugin) — instant, space-efficient, and without requiring `root@pam`.

## The Problem

The [proxmox-csi-plugin](https://github.com/sergelogvinov/proxmox-csi-plugin) provides Kubernetes CSI volume snapshots on Proxmox, but with significant limitations:

- **Snapshots are full copies** — the plugin uses Proxmox's content copy API (`zfs send | zfs recv`), which transfers all data. A snapshot of a 500 GB volume creates another 500 GB copy.
- **Requires `root@pam`** — the snapshot feature needs root access to the Proxmox API.
- **Slow** — copying large volumes takes minutes to hours.

ZFS natively supports instant, space-efficient snapshots and clones (copy-on-write), but the Proxmox API doesn't expose this — it always does a full data copy.

## The Solution

`pve-snapshot-api` sits in front of the Proxmox API and intercepts the storage content endpoints used by the CSI plugin. For ZFS pools, it replaces the slow `zfs send | recv` with instant `zfs snapshot` + `zfs clone`:

| | Without pve-snapshot-api | With pve-snapshot-api |
|---|---|---|
| **Snapshot speed** | Minutes to hours (full copy) | Sub-second (ZFS clone) |
| **Space usage** | Full duplicate | Only changed blocks |
| **Auth required** | `root@pam` | Any token with `Datastore.Allocate` |
| **Mechanism** | `zfs send \| zfs recv` | `zfs snapshot` + `zfs clone` (CoW) |

The API is fully compatible with Proxmox — same endpoints, same auth tokens, same UPID task responses. The CSI plugin doesn't need any changes. Non-ZFS requests are transparently proxied to the native Proxmox API.

## Features

- **True ZFS snapshots** — instant copy-on-write cloning for `zfspool` storage
- **proxmox-csi-plugin compatible** — drop-in, no CSI plugin changes needed
- **No `root@pam` required** — works with any PVE API token that has `Datastore.Allocate`
- **Proxmox API compatible** — same endpoints, same auth, same UPID responses
- **Transparent reverse proxy** — non-intercepted requests pass through to PVE API
- **Cluster-aware** — discovers cluster nodes and forwards requests to the correct node
- **Auth caching** — reduces PVE API calls with configurable TTL
- **TLS support** — uses PVE node certificates by default
- **systemd integration** — `Type=notify`, auto-restart on failure

## Requirements

- Proxmox VE 7+ or 8+
- ZFS-backed storage (`zfspool` type)
- Go 1.22+ (build only)
- PVE API token with `Datastore.Allocate` permission

## Installation

Add the APT repository and install:

```bash
curl -1sLf 'https://dl.cloudsmith.io/public/freshost/pve-snapshot-api/setup.deb.sh' | sudo -E bash
apt-get install pve-snapshot-api
```

By default the setup script enables the `stable` component. To use development builds instead, edit `/etc/apt/sources.list.d/freshost-pve-snapshot-api.list` and replace `stable` with `dev`, then run `apt-get update`.

The `.deb` installs the binary to `/usr/bin/pve-snapshot-api`, creates a default config at `/etc/pve-snapshot-api/config.yaml`, and enables the systemd service.

## Build from Source

```bash
# Build
make build

# Run tests
make test

# Build .deb package (on a Debian-based system)
make deb
dpkg -i ../pve-snapshot-api_*.deb
```

## Configuration

Default config path: `/etc/pve-snapshot-api/config.yaml`

```yaml
# Port to listen on (default: 8009)
listen_port: 8009

# Upstream Proxmox API URL (default: https://localhost:8006)
proxmox_api_url: "https://localhost:8006"

# Timeout for ZFS commands (default: 30s)
zfs_timeout: "30s"

# Timeout for PVE API auth requests (default: 15s)
pvesh_timeout: "15s"

# How long to cache auth results (default: 60s)
auth_cache_ttl: "60s"

# Log level: debug, info, warn, error (default: info)
log_level: "info"

# TLS certificate (defaults to PVE node cert)
tls:
  cert_file: "/etc/pve/local/pve-ssl.pem"
  key_file: "/etc/pve/local/pve-ssl.key"
```

## Usage with proxmox-csi-plugin

1. Install `pve-snapshot-api` on every PVE node in the cluster (see [Quick Start](#quick-start)).

2. Point the CSI plugin's cluster URL to port `8009` instead of `8006`:

```yaml
# proxmox-csi-plugin cluster config
clusters:
  - url: https://pve1.example.com:8009/api2/json
    username: csi@pve!csi-token
    token_name: csi-token
    token: "aaaa-bbbb-cccc-dddd"
```

3. Create your `VolumeSnapshotClass` and `VolumeSnapshot` resources as usual — the CSI plugin works unchanged, but snapshots are now instant ZFS clones instead of full copies.

No `root@pam` is needed. A dedicated API token with `Datastore.Allocate` on the relevant storage is sufficient.

## API

All intercepted endpoints follow the Proxmox API path format. Requests targeting non-ZFS storage are automatically proxied to Proxmox.

### Intercepted Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api2/json/nodes/{node}/storage/{storage}/content/{volume}` | Copy volume (ZFS snapshot + clone) |
| `DELETE` | `/api2/json/nodes/{node}/storage/{storage}/content/{disk}` | Delete volume (ZFS destroy) |
| `GET` | `/api2/json/nodes/{node}/tasks/{upid}/status` | Task status (local store, falls back to PVE) |
| `GET` | `/healthz` | Health check |

All other requests are proxied to the upstream Proxmox API.

### Authentication

Pass a PVE API token in the `Authorization` header:

```
Authorization: PVEAPIToken=user@realm!tokenid=secret-uuid
```

The token must have `Datastore.Allocate` permission on `/` or `/storage/{storage}`.

### Example: Copy a Volume

```bash
curl -X POST \
  -H "Authorization: PVEAPIToken=root@pam!csi=aaaa-bbbb-cccc-dddd" \
  -d "target=local-zfs:vm-200-disk-0" \
  https://pve1:8009/api2/json/nodes/pve1/storage/local-zfs/content/vm-100-disk-0
```

Response:

```json
{
  "data": "UPID:pve1:00001234:00005678:67890abc:imgcopy:vm-100-disk-0:root@pam:"
}
```

### Example: Delete a Volume

```bash
curl -X DELETE \
  -H "Authorization: PVEAPIToken=root@pam!csi=aaaa-bbbb-cccc-dddd" \
  https://pve1:8009/api2/json/nodes/pve1/storage/local-zfs/content/vm-100-disk-0
```

### Example: Check Task Status

```bash
curl https://pve1:8009/api2/json/nodes/pve1/tasks/UPID:pve1:00001234:.../status
```

Response:

```json
{
  "data": {
    "upid": "UPID:pve1:00001234:...",
    "node": "pve1",
    "status": "stopped",
    "exitstatus": "OK",
    "type": "imgcopy",
    "user": "root@pam",
    "id": "vm-100-disk-0"
  }
}
```

## Architecture

```
Client Request
      |
      v
  [Logging + Recovery Middleware]
      |
      v
  [Router]
      |
      ├── Intercepted (zfspool storage)
      │     ├── Auth (PVE HTTP API + cache)
      │     ├── Cluster routing (forward to correct node)
      │     └── ZFS operation (snapshot/clone/destroy)
      │
      └── Everything else
            └── Reverse proxy → Proxmox API (:8006)
```

### Cluster Routing

In a multi-node PVE cluster, the API discovers cluster members via `pvesh get /cluster/config/nodes` (refreshed every 30s). When a request targets a remote node, it is forwarded to that node's instance of `pve-snapshot-api` on the same port. The `X-Forwarded-Node` header prevents routing loops.

### What Uses pvesh (local CLI)

- **Cluster discovery** — `pvesh get /cluster/config/nodes` (runs locally, no auth needed)
- **Pool resolver** — `pvesh get /storage/{name}` (resolves storage name to ZFS pool path)

### What Uses PVE HTTP API

- **Token validation** — `GET /api2/json/access/permissions` (requires the user's token)

## Project Structure

```
cmd/pve-snapshot-api/   Entry point, wiring
pkg/
  api/                  HTTP router, handlers, middleware, PVE reverse proxy
  auth/                 Token validation via PVE HTTP API + auth cache
  cluster/              Cluster node discovery via pvesh
  config/               YAML config loading with defaults
  pool/                 Storage name → ZFS pool path resolver via pvesh
  proxy/                Inter-node request forwarding (cluster-aware)
  pveapi/               Proxmox-compatible JSON response helpers
  storage/              StorageBackend interface
    zfs/                ZFS CLI backend (snapshot, clone, destroy, list, promote)
  task/                 UPID generation + in-memory task result store
debian/                 Debian packaging (systemd service, postinst, control)
```

## Development

```bash
# Build binary
make build

# Run tests
make test

# Run go vet
make vet

# Clean build artifacts
make clean
```

## License

See [LICENSE](LICENSE) for details.
