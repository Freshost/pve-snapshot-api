package zfs

import (
	"context"
	"os/exec"
	"time"

	"github.com/freshost/pve-snapshot-api/pkg/config"
)

// CommandRunner abstracts command execution for testability.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// DefaultRunner executes commands via os/exec.
func DefaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// ZFSBackend implements storage operations using ZFS commands.
type ZFSBackend struct {
	timeout time.Duration
	run     CommandRunner
}

// New creates a ZFSBackend with the given config and command runner.
func New(cfg *config.Config, runner CommandRunner) *ZFSBackend {
	if runner == nil {
		runner = DefaultRunner
	}
	return &ZFSBackend{
		timeout: cfg.ZFSTimeout,
		run:     runner,
	}
}

func (z *ZFSBackend) runZFS(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, z.timeout)
	defer cancel()
	return z.run(ctx, "zfs", args...)
}
