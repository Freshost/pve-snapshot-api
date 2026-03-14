package storage

import (
	"context"
	"time"
)

// CommandRunner abstracts command execution for testability.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type Snapshot struct {
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
	Used    string    `json:"used"`
	Clones  []string  `json:"clones,omitempty"`
}

type VolumeInfo struct {
	Name        string `json:"name"`
	Size        string `json:"size"`
	Used        string `json:"used"`
	StorageType string `json:"storage_type"`
}

type StorageBackend interface {
	CreateSnapshot(ctx context.Context, volid, snapname string) error
	DeleteSnapshot(ctx context.Context, volid, snapname string) error
	CloneSnapshot(ctx context.Context, volid, snapname, target string) error
	ListSnapshots(ctx context.Context, volid string) ([]Snapshot, error)
	PromoteClone(ctx context.Context, volid string) error
	GetVolumeInfo(ctx context.Context, volid string) (*VolumeInfo, error)
	DestroyVolume(ctx context.Context, volid string) error
	GetOriginSnapshot(ctx context.Context, volid string) (string, error)
}
