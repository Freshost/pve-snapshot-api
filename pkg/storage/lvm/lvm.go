package lvm

import (
	"context"
	"fmt"

	"github.com/freshost/pve-snapshot-api/pkg/storage"
)

type LVMBackend struct{}

func New() *LVMBackend {
	return &LVMBackend{}
}

func (l *LVMBackend) CreateSnapshot(_ context.Context, _, _ string) error {
	return fmt.Errorf("LVM storage backend not implemented")
}

func (l *LVMBackend) DeleteSnapshot(_ context.Context, _, _ string) error {
	return fmt.Errorf("LVM storage backend not implemented")
}

func (l *LVMBackend) CloneSnapshot(_ context.Context, _, _, _ string) error {
	return fmt.Errorf("LVM storage backend not implemented")
}

func (l *LVMBackend) ListSnapshots(_ context.Context, _ string) ([]storage.Snapshot, error) {
	return nil, fmt.Errorf("LVM storage backend not implemented")
}

func (l *LVMBackend) PromoteClone(_ context.Context, _ string) error {
	return fmt.Errorf("LVM storage backend not implemented")
}

func (l *LVMBackend) GetVolumeInfo(_ context.Context, _ string) (*storage.VolumeInfo, error) {
	return nil, fmt.Errorf("LVM storage backend not implemented")
}

func (l *LVMBackend) DestroyVolume(_ context.Context, _ string) error {
	return fmt.Errorf("LVM storage backend not implemented")
}

func (l *LVMBackend) GetOriginSnapshot(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("LVM storage backend not implemented")
}
