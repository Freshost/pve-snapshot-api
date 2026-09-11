package zfs

import (
	"context"
	"fmt"
	"strings"

	"github.com/freshost/pve-snapshot-api/pkg/storage"
)

func (z *ZFSBackend) DestroyVolume(ctx context.Context, volid string) error {
	z.mu.Lock()
	defer z.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	return z.destroyVolume(ctx, volid)
}

func (z *ZFSBackend) GetOriginSnapshot(ctx context.Context, volid string) (string, error) {
	out, err := z.runZFS(ctx, "get", "-Hp", "-o", "value", "origin", volid)
	if err != nil {
		return "", err
	}
	val := strings.TrimSpace(string(out))
	if val == "" || val == "-" {
		return "", nil
	}
	return val, nil
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
