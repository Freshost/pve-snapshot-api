package zfs

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/freshost/pve-snapshot-api/pkg/volume"
)

var datasetPath = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.:-]*(/[a-zA-Z0-9][a-zA-Z0-9_.:-]*)+$`)

type dataset struct{ name, kind, origin, guid, sourceGUID string }

func validateDisk(ds string) error {
	if !datasetPath.MatchString(ds) || !volume.ValidName(path.Base(ds)) {
		return fmt.Errorf("unsupported volume dataset %q", ds)
	}
	return nil
}
func (z *ZFSBackend) datasets(ctx context.Context, ds string) (map[string]dataset, error) {
	root := strings.SplitN(ds, "/", 2)[0]
	out, err := z.runZFS(ctx, "list", "-H", "-p", "-r", "-t", "volume,filesystem", "-o", "name,type,origin,guid,pve-snapshot-api:source-guid", root)
	if err != nil {
		return nil, fmt.Errorf("read ZFS topology: %s: %w", out, err)
	}
	result := map[string]dataset{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			return nil, fmt.Errorf("invalid ZFS topology output")
		}
		result[f[0]] = dataset{f[0], f[1], f[2], f[3], f[4]}
	}
	return result, nil
}
func (z *ZFSBackend) CopyVolume(ctx context.Context, source, target string) error {
	z.mu.Lock()
	defer z.mu.Unlock()
	if err := validateDisk(source); err != nil {
		return err
	}
	if err := validateDisk(target); err != nil {
		return err
	}
	if source == target || path.Dir(source) != path.Dir(target) {
		return fmt.Errorf("copy requires different volumes in the same storage dataset")
	}
	graph, err := z.datasets(ctx, source)
	if err != nil {
		return err
	}
	src, ok := graph[source]
	if !ok || src.kind != "volume" || src.guid == "" || src.guid == "-" {
		return fmt.Errorf("source is not a ZFS volume")
	}
	if err := z.reconcileSnapshots(ctx, source); err != nil {
		return err
	}
	if dst, exists := graph[target]; exists {
		if dst.kind == "volume" && dst.sourceGUID == src.guid {
			return nil
		}
		return fmt.Errorf("target already exists with different or unknown provenance")
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	snap := fmt.Sprintf("%s@psa-%x", source, nonce)
	if out, err := z.runZFS(ctx, "snapshot", "-o", "pve-snapshot-api:managed=1", snap); err != nil {
		return fmt.Errorf("create snapshot: %s: %w", out, err)
	}
	// A deferred destroy removes an unreferenced snapshot on failure, or marks it
	// for automatic removal when the last clone is removed. It survives promotion.
	out, cloneErr := z.runZFS(ctx, "clone", "-o", "pve-snapshot-api:source-guid="+src.guid, snap, target)
	if cloneErr != nil {
		cloneErr = fmt.Errorf("clone volume (retry to reconcile): %s: %w", out, cloneErr)
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), z.timeout)
	defer cancel()
	out, cleanupErr := z.runZFS(cleanup, "destroy", "-d", snap)
	if cleanupErr != nil {
		cleanupErr = fmt.Errorf("defer snapshot cleanup (retry to reconcile): %s: %w", out, cleanupErr)
	}
	return errors.Join(cloneErr, cleanupErr)
}

func (z *ZFSBackend) destroyVolume(ctx context.Context, volid string) error {
	if err := validateDisk(volid); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		graph, err := z.datasets(ctx, volid)
		if err != nil {
			return err
		}
		current, exists := graph[volid]
		if !exists {
			return nil
		}
		if current.kind != "volume" {
			return fmt.Errorf("refusing to destroy a filesystem")
		}
		// Promote one actual dependent, then refresh. A promote moves snapshot
		// ownership and may change every remaining origin in this graph.
		dependent := ""
		for name, d := range graph {
			if strings.HasPrefix(d.origin, volid+"@") {
				if d.kind != "volume" || validateDisk(name) != nil {
					return fmt.Errorf("unsupported dependent clone %s", name)
				}
				dependent = name
				break
			}
		}
		if dependent != "" {
			if out, err := z.runZFS(ctx, "promote", dependent); err != nil {
				return fmt.Errorf("promote %s: %s: %w", dependent, out, err)
			}
			continue
		}
		// Delete only this volume's snapshots. Never recursively delete descendants
		// or dependent clones; native ZFS checks reject concurrent dependency changes.
		out, err := z.runZFS(ctx, "list", "-H", "-o", "name", "-t", "snapshot", "-d", "1", volid)
		if err != nil {
			return fmt.Errorf("list volume snapshots: %s: %w", out, err)
		}
		for _, snap := range strings.Fields(string(out)) {
			if !strings.HasPrefix(snap, volid+"@") {
				return fmt.Errorf("unexpected child snapshot %s", snap)
			}
			if out, err := z.runZFS(ctx, "destroy", snap); err != nil {
				return fmt.Errorf("destroy snapshot: %s: %w", out, err)
			}
		}
		if out, err := z.runZFS(ctx, "destroy", volid); err != nil {
			return fmt.Errorf("destroy volume: %s: %w", out, err)
		}
		return nil
	}
}

// OperationTimeout is separate from individual command timeouts.
const OperationTimeout = 5 * time.Minute

// Reconcile snapshots left between snapshot/clone/cleanup by a process crash.
// Native deferred deletion preserves every extant clone. Only our explicitly
// tagged, randomly named snapshots are eligible; user/PVE snapshots are untouched.
func (z *ZFSBackend) reconcileSnapshots(ctx context.Context, source string) error {
	out, err := z.runZFS(ctx, "list", "-H", "-o", "name,pve-snapshot-api:managed", "-t", "snapshot", "-d", "1", source)
	if err != nil {
		return fmt.Errorf("read managed snapshots: %s: %w", out, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 2 {
			return fmt.Errorf("invalid snapshot metadata")
		}
		if !strings.HasPrefix(fields[0], source+"@psa-") || fields[1] != "1" {
			continue
		}
		if out, err := z.runZFS(ctx, "destroy", "-d", fields[0]); err != nil {
			return fmt.Errorf("reconcile snapshot: %s: %w", out, err)
		}
	}
	return nil
}
