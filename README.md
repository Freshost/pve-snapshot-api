# pve-snapshot-api

[![CI](https://github.com/Freshost/pve-snapshot-api/actions/workflows/ci.yml/badge.svg)](https://github.com/Freshost/pve-snapshot-api/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Freshost/pve-snapshot-api)](https://github.com/Freshost/pve-snapshot-api/releases)

Proxmox API middleware for ZFS copy-on-write snapshots and clones, designed for
[proxmox-csi-plugin](https://github.com/sergelogvinov/proxmox-csi-plugin).
It replaces supported content-copy requests with ZFS snapshot/clone operations
and proxies other requests to Proxmox.

## Requirements

- Proxmox VE with `zfspool` storage and ZFS VM volumes
  (`vm-<id>-<name>`, including CSI PVC names).
- A PVE API token with `Datastore.Allocate` on the relevant storage.
- Installation on each node used by the middleware.

Copies must stay on the same storage and target node. ZFS filesystems (`subvol-*`)
and cross-storage copies are unsupported. See [compatibility](docs/compatibility.md).

## Install

```sh
curl -1sLf 'https://dl.cloudsmith.io/public/freshost/pve-snapshot-api/setup.deb.sh' | sudo -E bash
sudo apt-get install pve-snapshot-api
```

Packages are also available from [GitHub Releases](https://github.com/Freshost/pve-snapshot-api/releases).
The Debian package enables the systemd service and installs its configuration at
`/etc/pve-snapshot-api/config.yaml`.

## Configure

The defaults listen on port `8009`, forward to `https://localhost:8006`, and use
PVE node certificates with `/etc/pve/pve-root-ca.pem` as the CA. See the complete
[example configuration](configs/config.yaml) for timeouts, TLS and task storage.
Configure `ca_file` for a different CA; an invalid configured CA fails startup.

Point the CSI driver to port `8009`:

```yaml
clusters:
  - url: https://pve.example.com:8009/api2/json
    insecure: false
    token_id: "csi@pve!example"
    token_secret: "REPLACE_WITH_TOKEN_SECRET"
    region: "example-cluster"
```

The CSI driver also needs permissions for its own provisioning and VM operations;
follow its [configuration guide](https://github.com/sergelogvinov/proxmox-csi-plugin/blob/main/docs/config.md).
Validate snapshot, restore and deletion with your CSI version before production use.

## API

| Method | Path | Operation |
| --- | --- | --- |
| POST | `/api2/json/nodes/{node}/storage/{storage}/content/{volume}` | Clone volume |
| DELETE | `/api2/json/nodes/{node}/storage/{storage}/content/{volume}` | Delete volume |
| GET | `/api2/json/nodes/{node}/tasks/{upid}/status` | Task status |
| GET | `/healthz` | Process health |

For example, with `PVE_API_TOKEN` set to your full `user@realm!token=secret` value:

```sh
curl --cacert /path/to/pve-ca.pem \
  -H "Authorization: PVEAPIToken=$PVE_API_TOKEN" \
  -d 'target=local-zfs:vm-200-disk-0' \
  https://pve.example.com:8009/api2/json/nodes/pve1/storage/local-zfs/content/vm-100-disk-0
```

Copy and delete return a UPID in `data`; poll its status through this service.
Task status requires the owning token or `Sys.Audit` on the target node.
Other endpoints and non-ZFS operations pass through to Proxmox.

## Development

Use the Go version in [go.mod](go.mod).

```sh
make build
make test
make vet
make release-check
```

See [Contributing](CONTRIBUTING.md), [Changelog](CHANGELOG.md),
[Releasing](docs/releasing.md) and [Security](SECURITY.md).

## License

[Apache-2.0](LICENSE). Package hosting provided by [Cloudsmith](https://cloudsmith.com).
