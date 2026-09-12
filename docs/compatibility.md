# Compatibility

## Supported operations

- ZFS VM block volumes named `vm-<positive-vmid>-<name>`, including CSI PVC and
  snapshot names, with an optional matching storage prefix. Names use ASCII
  letters, digits, dots, underscores and hyphens; the suffix starts with a letter
  or digit. Copies stay on the same storage and target node.
- Requests may enter through another cluster node running the middleware.
- Non-ZFS requests pass through to Proxmox. Unsupported ZFS names and destinations
  fail explicitly; use the native API for filesystem volumes and other copy modes.
- Retries recognize a clone by its source GUID. Deletion follows native origins
  and promotes dependent clones without recursive dataset deletion.

## Runtime behavior

Tasks persist for seven days, bounded to 10,000 records. A restart marks unfinished
tasks interrupted; retry reconciles managed copy state. Client disconnection does
not cancel accepted mutations. Operations have a five-minute limit plus cleanup.
Synthetic task status must be polled through this service; native PVE task lists
and logs do not include these tasks.

Permission caching delays revocation by `auth_cache_ttl`; use `0s` to disable it.
The service does not acquire PVE storage locks: avoid concurrent changes to the
same volume through native PVE or ZFS commands. Certificate files reload for new
TLS connections; system roots and the configured CA are trusted.

## Upgrading from 0.1

Old in-memory task history cannot be recovered. Existing clones remain readable,
but targets without matching source-GUID metadata are rejected as retries.
Legacy untagged snapshots are not automatically reconciled. Ensure the configured
CA exists and the service can write its private task-state directory.

## Testing

CI checks both architectures, permissions, routing, task recovery and 24 native
ZFS deletion orders for both disk and CSI volume names. Run
`sudo env "PATH=$PATH" sh scripts/test-zfs.sh` on an isolated Linux host with ZFS; it creates and removes its own file-backed pool.
Validate the complete CSI snapshot/restore/delete workflow and multi-node routing
for your deployment. Full PVE API equivalence is not claimed.
