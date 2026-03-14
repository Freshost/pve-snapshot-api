package zfs

import (
	"context"
	"fmt"
	"strings"

	"github.com/freshost/pve-snapshot-api/pkg/storage"
)

func (z *ZFSBackend) DestroyVolume(ctx context.Context, volid string) error {
	// Find dependent clones via user property and promote them first
	out, err := z.runZFS(ctx, "list", "-H", "-o", "name,pve-snapshot-api:parent", "-t", "volume")
	if err != nil && !strings.Contains(string(out), "no datasets available") {
		// Non-fatal: if listing fails we still try to destroy
	} else {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			fields := strings.SplitN(line, "\t", 2)
			if len(fields) != 2 {
				continue
			}
			parent := strings.TrimSpace(fields[1])
			// Check if parent references a snapshot of this volume
			if strings.HasPrefix(parent, volid+"@") {
				cloneName := strings.TrimSpace(fields[0])
				if _, promErr := z.runZFS(ctx, "promote", cloneName); promErr != nil {
					return fmt.Errorf("promoting dependent clone %s: %w", cloneName, promErr)
				}
			}
		}
	}

	// Destroy all snapshots of this volume first
	z.runZFS(ctx, "destroy", "-r", volid)

	// Destroy the volume itself
	out, err = z.runZFS(ctx, "destroy", volid)
	if err != nil {
		if strings.Contains(string(out), "dataset does not exist") {
			return nil // idempotent
		}
		return fmt.Errorf("zfs destroy %s: %s: %w", volid, string(out), err)
	}
	return nil
}

func (z *ZFSBackend) GetVolumeInfo(ctx context.Context, volid string) (*storage.VolumeInfo, error) {
	out, err := z.runZFS(ctx, "get", "-Hp", "-o", "value", "volsize,used", volid)
	if err != nil {
		if strings.Contains(string(out), "dataset does not exist") {
			return nil, fmt.Errorf("volume %s not found", volid)
		}
		return nil, fmt.Errorf("getting volume info %s: %s: %w", volid, string(out), err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("unexpected zfs get output for %s", volid)
	}

	return &storage.VolumeInfo{
		Name:        volid,
		Size:        strings.TrimSpace(lines[0]),
		Used:        strings.TrimSpace(lines[1]),
		StorageType: "zfs",
	}, nil
}
