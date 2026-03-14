package zfs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/freshost/pve-snapshot-api/pkg/storage"
)

func (z *ZFSBackend) CreateSnapshot(ctx context.Context, volid, snapname string) error {
	dataset := fmt.Sprintf("%s@%s", volid, snapname)
	out, err := z.runZFS(ctx, "snapshot", dataset)
	if err != nil {
		if strings.Contains(string(out), "dataset already exists") {
			return nil // idempotent
		}
		return fmt.Errorf("zfs snapshot %s: %s: %w", dataset, string(out), err)
	}
	return nil
}

func (z *ZFSBackend) DeleteSnapshot(ctx context.Context, volid, snapname string) error {
	dataset := fmt.Sprintf("%s@%s", volid, snapname)

	// Find dependent clones via user property
	out, err := z.runZFS(ctx, "list", "-H", "-o", "name,pve-snapshot-api:parent", "-t", "volume")
	if err != nil {
		// If no volumes exist, that's fine
		if !strings.Contains(string(out), "no datasets available") {
			return fmt.Errorf("listing clones: %s: %w", string(out), err)
		}
	}

	// Promote any clones that depend on this snapshot
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) != 2 {
			continue
		}
		parent := strings.TrimSpace(fields[1])
		if parent == dataset {
			cloneName := strings.TrimSpace(fields[0])
			if _, err := z.runZFS(ctx, "promote", cloneName); err != nil {
				return fmt.Errorf("promoting clone %s: %w", cloneName, err)
			}
		}
	}

	out, err = z.runZFS(ctx, "destroy", dataset)
	if err != nil {
		if strings.Contains(string(out), "could not find any snapshots") ||
			strings.Contains(string(out), "dataset does not exist") {
			return nil // idempotent
		}
		return fmt.Errorf("zfs destroy %s: %s: %w", dataset, string(out), err)
	}
	return nil
}

func (z *ZFSBackend) ListSnapshots(ctx context.Context, volid string) ([]storage.Snapshot, error) {
	out, err := z.runZFS(ctx, "list", "-H", "-o", "name,creation,used", "-t", "snapshot", "-r", volid)
	if err != nil {
		if strings.Contains(string(out), "dataset does not exist") {
			return nil, fmt.Errorf("volume %s not found", volid)
		}
		return nil, fmt.Errorf("listing snapshots: %s: %w", string(out), err)
	}

	var snapshots []storage.Snapshot
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}

		fullName := strings.TrimSpace(fields[0])
		parts := strings.SplitN(fullName, "@", 2)
		if len(parts) != 2 {
			continue
		}
		snapName := parts[1]

		created, _ := time.Parse("Mon Jan _2 15:04 2006", strings.TrimSpace(fields[1]))

		snap := storage.Snapshot{
			Name:    snapName,
			Created: created,
			Used:    strings.TrimSpace(fields[2]),
		}

		// Get clones for this snapshot
		clones, _ := z.getClones(ctx, fullName)
		snap.Clones = clones

		snapshots = append(snapshots, snap)
	}

	return snapshots, nil
}

func (z *ZFSBackend) getClones(ctx context.Context, snapDataset string) ([]string, error) {
	out, err := z.runZFS(ctx, "list", "-H", "-o", "name,pve-snapshot-api:parent", "-t", "volume")
	if err != nil {
		return nil, err
	}

	var clones []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) != 2 {
			continue
		}
		if strings.TrimSpace(fields[1]) == snapDataset {
			clones = append(clones, strings.TrimSpace(fields[0]))
		}
	}
	return clones, nil
}
