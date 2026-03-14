package pool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// CommandRunner abstracts command execution for testability.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type storageInfo struct {
	Pool string
	Type string
}

// Resolver resolves PVE storage names to ZFS pool paths and storage types.
type Resolver struct {
	timeout time.Duration
	run     CommandRunner
	mu      sync.RWMutex
	cache   map[string]*storageInfo
}

// New creates a Resolver with the given timeout and command runner.
func New(timeout time.Duration, runner CommandRunner) *Resolver {
	return &Resolver{
		timeout: timeout,
		run:     runner,
		cache:   make(map[string]*storageInfo),
	}
}

// pveshStorage represents the JSON response from pvesh get /storage/{name}.
type pveshStorage struct {
	Pool string `json:"pool"`
	Type string `json:"type"`
}

func (r *Resolver) fetch(ctx context.Context, storageName string) (*storageInfo, error) {
	r.mu.RLock()
	if info, ok := r.cache[storageName]; ok {
		r.mu.RUnlock()
		return info, nil
	}
	r.mu.RUnlock()

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	out, err := r.run(ctx, "pvesh", "get", fmt.Sprintf("/storage/%s", storageName), "--output-format", "json")
	if err != nil {
		return nil, fmt.Errorf("pvesh get /storage/%s: %s: %w", storageName, string(out), err)
	}

	var s pveshStorage
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, fmt.Errorf("parsing storage %s: %w", storageName, err)
	}

	info := &storageInfo{Pool: s.Pool, Type: s.Type}

	r.mu.Lock()
	r.cache[storageName] = info
	r.mu.Unlock()

	return info, nil
}

// Resolve returns the ZFS pool path for a given PVE storage name.
func (r *Resolver) Resolve(ctx context.Context, storageName string) (string, error) {
	info, err := r.fetch(ctx, storageName)
	if err != nil {
		return "", err
	}
	return info.Pool, nil
}

// StorageType returns the storage type (e.g. "zfspool", "lvm") for a PVE storage name.
func (r *Resolver) StorageType(ctx context.Context, storageName string) (string, error) {
	info, err := r.fetch(ctx, storageName)
	if err != nil {
		return "", err
	}
	return info.Type, nil
}

// VolumeToDataset converts a PVE volume ID (e.g. "vm-100-disk-0") to a full
// ZFS dataset path (e.g. "rpool/data/vm-100-disk-0") using the storage's pool.
func (r *Resolver) VolumeToDataset(ctx context.Context, storageName, volumeID string) (string, error) {
	pool, err := r.Resolve(ctx, storageName)
	if err != nil {
		return "", err
	}
	return pool + "/" + volumeID, nil
}
