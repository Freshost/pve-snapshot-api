package pool

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type CommandRunner func(context.Context, string, ...string) ([]byte, error)
type Resolver struct {
	timeout time.Duration
	run     CommandRunner
}
type Info struct {
	Pool    string `json:"pool"`
	Type    string `json:"type"`
	Nodes   string `json:"nodes"`
	Disable int    `json:"disable"`
}

var validStorage = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
var validVolume = regexp.MustCompile(`^vm-[1-9][0-9]*-disk-[0-9]+$`)
var validDataset = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.:-]*(/[a-zA-Z0-9][a-zA-Z0-9_.:-]*)*$`)

func New(timeout time.Duration, run CommandRunner) *Resolver {
	return &Resolver{timeout: timeout, run: run}
}

// Fetch deliberately reads fresh configuration for every operation. Never cache a
// mapping used for destructive operations across requests.
func (r *Resolver) Fetch(ctx context.Context, name string) (*Info, error) {
	if !validStorage.MatchString(name) {
		return nil, fmt.Errorf("invalid storage ID")
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	out, err := r.run(ctx, "pvesh", "get", "/storage/"+name, "--output-format", "json")
	if err != nil {
		return nil, fmt.Errorf("storage %s: %s: %w", name, out, err)
	}
	var info Info
	if err = json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("parsing storage: %w", err)
	}
	if info.Type == "" {
		return nil, fmt.Errorf("missing storage type")
	}
	return &info, nil
}
func (i *Info) Available(node string) error {
	if i.Disable != 0 {
		return fmt.Errorf("storage is disabled")
	}
	if i.Nodes != "" {
		for _, n := range strings.Split(i.Nodes, ",") {
			if n == node {
				return nil
			}
		}
		return fmt.Errorf("storage unavailable on node %s", node)
	}
	return nil
}
func NormalizeVolume(storage, id string) (string, error) {
	if prefix, name, ok := strings.Cut(id, ":"); ok {
		if prefix != storage {
			return "", fmt.Errorf("storage ID mismatch")
		}
		id = name
	}
	if !validVolume.MatchString(id) {
		return "", fmt.Errorf("only vm-<id>-disk-<index> ZFS volumes are supported")
	}
	return id, nil
}
func (i *Info) Dataset(volume string) (string, error) {
	if i.Type != "zfspool" || !validDataset.MatchString(i.Pool) || !validVolume.MatchString(volume) {
		return "", fmt.Errorf("invalid ZFS pool or volume")
	}
	return i.Pool + "/" + volume, nil
}
func (r *Resolver) Resolve(ctx context.Context, name string) (string, error) {
	i, e := r.Fetch(ctx, name)
	if e != nil {
		return "", e
	}
	return i.Pool, nil
}
func (r *Resolver) StorageType(ctx context.Context, name string) (string, error) {
	i, e := r.Fetch(ctx, name)
	if e != nil {
		return "", e
	}
	return i.Type, nil
}
func (r *Resolver) VolumeToDataset(ctx context.Context, name, id string) (string, error) {
	i, e := r.Fetch(ctx, name)
	if e != nil {
		return "", e
	}
	v, e := NormalizeVolume(name, id)
	if e != nil {
		return "", e
	}
	return i.Dataset(v)
}
