package zfs

import (
	"context"
	"fmt"
	"strings"
)

func (z *ZFSBackend) CloneSnapshot(ctx context.Context, volid, snapname, target string) error {
	dataset := fmt.Sprintf("%s@%s", volid, snapname)
	out, err := z.runZFS(ctx, "clone", dataset, target)
	if err != nil {
		return fmt.Errorf("zfs clone %s -> %s: %s: %w", dataset, target, string(out), err)
	}

	// Set user property to track parent snapshot
	prop := fmt.Sprintf("pve-snapshot-api:parent=%s", dataset)
	out, err = z.runZFS(ctx, "set", prop, target)
	if err != nil {
		return fmt.Errorf("setting parent property on %s: %s: %w", target, string(out), err)
	}

	return nil
}

func (z *ZFSBackend) PromoteClone(ctx context.Context, volid string) error {
	out, err := z.runZFS(ctx, "promote", volid)
	if err != nil {
		if strings.Contains(string(out), "not a cloned filesystem") {
			return fmt.Errorf("volume %s is not a clone", volid)
		}
		return fmt.Errorf("zfs promote %s: %s: %w", volid, string(out), err)
	}
	return nil
}
