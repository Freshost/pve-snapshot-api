package zfs

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/freshost/pve-snapshot-api/pkg/config"
	"github.com/freshost/pve-snapshot-api/pkg/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// call records a single invocation of the mock CommandRunner.
type call struct {
	Name string
	Args []string
}

// mockRunner is a configurable mock for CommandRunner.
// It records every invocation and returns pre-configured results.
type mockRunner struct {
	calls   []call
	results []mockResult
}

// mockResult pairs output and error for a single call.
type mockResult struct {
	output []byte
	err    error
}

// run implements CommandRunner. It records the call and returns the next
// configured result. If no results remain it returns nil, nil.
func (m *mockRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	m.calls = append(m.calls, call{Name: name, Args: args})
	if len(m.results) == 0 {
		return nil, nil
	}
	r := m.results[0]
	m.results = m.results[1:]
	return r.output, r.err
}

// newTestBackend creates a ZFSBackend wired to the given mockRunner.
func newTestBackend(m *mockRunner) *ZFSBackend {
	cfg := &config.Config{
		ZFSTimeout: 5 * time.Second,
	}
	return New(cfg, m.run)
}

// ---------------------------------------------------------------------------
// CreateSnapshot
// ---------------------------------------------------------------------------

func TestCreateSnapshot_Success(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			{output: nil, err: nil},
		},
	}
	b := newTestBackend(m)

	err := b.CreateSnapshot(context.Background(), "rpool/data/vm-100-disk-0", "snap1")
	require.NoError(t, err)

	require.Len(t, m.calls, 1)
	c := m.calls[0]
	assert.Equal(t, "zfs", c.Name)
	assert.Equal(t, []string{"snapshot", "rpool/data/vm-100-disk-0@snap1"}, c.Args)
}

func TestCreateSnapshot_Idempotent_DatasetAlreadyExists(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			{
				output: []byte("cannot create snapshot 'rpool/data/vm-100-disk-0@snap1': dataset already exists"),
				err:    fmt.Errorf("exit status 1"),
			},
		},
	}
	b := newTestBackend(m)

	err := b.CreateSnapshot(context.Background(), "rpool/data/vm-100-disk-0", "snap1")
	require.NoError(t, err, "dataset already exists should be treated as idempotent success")
}

func TestCreateSnapshot_Error(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			{
				output: []byte("cannot open 'rpool/data/vm-100-disk-0': dataset does not exist"),
				err:    fmt.Errorf("exit status 1"),
			},
		},
	}
	b := newTestBackend(m)

	err := b.CreateSnapshot(context.Background(), "rpool/data/vm-100-disk-0", "snap1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zfs snapshot")
	assert.Contains(t, err.Error(), "dataset does not exist")
}

// ---------------------------------------------------------------------------
// DeleteSnapshot
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// CloneSnapshot
// ---------------------------------------------------------------------------

func TestCloneSnapshot_Success(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			// clone
			{output: nil, err: nil},
			// set property
			{output: nil, err: nil},
		},
	}
	b := newTestBackend(m)

	err := b.CloneSnapshot(context.Background(), "rpool/data/vm-100-disk-0", "snap1", "rpool/data/vm-200-disk-0")
	require.NoError(t, err)

	require.Len(t, m.calls, 1)

	// Verify clone command args
	assert.Equal(t, "zfs", m.calls[0].Name)
	assert.Equal(t, []string{"clone", "rpool/data/vm-100-disk-0@snap1", "rpool/data/vm-200-disk-0"}, m.calls[0].Args)

}

func TestCloneSnapshot_CloneError(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			{
				output: []byte("cannot create 'rpool/data/vm-200-disk-0': dataset already exists"),
				err:    fmt.Errorf("exit status 1"),
			},
		},
	}
	b := newTestBackend(m)

	err := b.CloneSnapshot(context.Background(), "rpool/data/vm-100-disk-0", "snap1", "rpool/data/vm-200-disk-0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zfs clone")
}

// ---------------------------------------------------------------------------
// ListSnapshots
// ---------------------------------------------------------------------------

func TestListSnapshots_ParsesOutput(t *testing.T) {
	snapListOutput := strings.Join([]string{
		"rpool/data/vm-100-disk-0@daily\tMon Jan  6 10:30 2025\t128K",
		"rpool/data/vm-100-disk-0@weekly\tSun Jan  5 00:00 2025\t256K",
	}, "\n")

	// For each snapshot, getClones is called to find dependent clones.
	// First snapshot has one clone, second has none.
	clonesOutput1 := "rpool/data/vm-300-disk-0\n"
	clonesOutput2 := "-\n"

	m := &mockRunner{
		results: []mockResult{
			// list snapshots
			{output: []byte(snapListOutput), err: nil},
			// getClones for first snapshot
			{output: []byte(clonesOutput1), err: nil},
			// getClones for second snapshot
			{output: []byte(clonesOutput2), err: nil},
		},
	}
	b := newTestBackend(m)

	snaps, err := b.ListSnapshots(context.Background(), "rpool/data/vm-100-disk-0")
	require.NoError(t, err)
	require.Len(t, snaps, 2)

	// First snapshot
	assert.Equal(t, "daily", snaps[0].Name)
	assert.Equal(t, "128K", snaps[0].Used)
	assert.Equal(t, 2025, snaps[0].Created.Year())
	assert.Equal(t, time.January, snaps[0].Created.Month())
	assert.Equal(t, 6, snaps[0].Created.Day())
	require.Len(t, snaps[0].Clones, 1)
	assert.Equal(t, "rpool/data/vm-300-disk-0", snaps[0].Clones[0])

	// Second snapshot
	assert.Equal(t, "weekly", snaps[1].Name)
	assert.Equal(t, "256K", snaps[1].Used)
	assert.Nil(t, snaps[1].Clones)
}

func TestListSnapshots_CorrectArgs(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			{output: []byte(""), err: nil},
		},
	}
	b := newTestBackend(m)

	_, err := b.ListSnapshots(context.Background(), "rpool/data/vm-100-disk-0")
	require.NoError(t, err)

	require.Len(t, m.calls, 1)
	assert.Equal(t, []string{"list", "-H", "-o", "name,creation,used", "-t", "snapshot", "-r", "rpool/data/vm-100-disk-0"}, m.calls[0].Args)
}

func TestListSnapshots_EmptyOutput(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			{output: []byte(""), err: nil},
		},
	}
	b := newTestBackend(m)

	snaps, err := b.ListSnapshots(context.Background(), "rpool/data/vm-100-disk-0")
	require.NoError(t, err)
	assert.Empty(t, snaps)
}

func TestListSnapshots_DatasetNotFound(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			{
				output: []byte("cannot open 'rpool/data/vm-999-disk-0': dataset does not exist"),
				err:    fmt.Errorf("exit status 1"),
			},
		},
	}
	b := newTestBackend(m)

	snaps, err := b.ListSnapshots(context.Background(), "rpool/data/vm-999-disk-0")
	require.Error(t, err)
	assert.Nil(t, snaps)
	assert.Contains(t, err.Error(), "volume rpool/data/vm-999-disk-0 not found")
}

func TestListSnapshots_SkipsMalformedLines(t *testing.T) {
	// One valid line, one with missing fields, one with no @ separator
	snapListOutput := strings.Join([]string{
		"rpool/data/vm-100-disk-0@good\tMon Jan  6 10:30 2025\t128K",
		"badline_without_tabs",
		"rpool/data/vm-100-disk-0\tMon Jan  6 10:30 2025\t128K", // no @ in name
	}, "\n")

	m := &mockRunner{
		results: []mockResult{
			{output: []byte(snapListOutput), err: nil},
			// getClones for the one valid snapshot
			{output: []byte(""), err: nil},
		},
	}
	b := newTestBackend(m)

	snaps, err := b.ListSnapshots(context.Background(), "rpool/data/vm-100-disk-0")
	require.NoError(t, err)
	require.Len(t, snaps, 1)
	assert.Equal(t, "good", snaps[0].Name)
}

// ---------------------------------------------------------------------------
// GetVolumeInfo
// ---------------------------------------------------------------------------

func TestGetVolumeInfo_ParsesOutput(t *testing.T) {
	// zfs get -Hp returns one value per line
	output := "10737418240\n5368709120\n"

	m := &mockRunner{
		results: []mockResult{
			{output: []byte(output), err: nil},
		},
	}
	b := newTestBackend(m)

	info, err := b.GetVolumeInfo(context.Background(), "rpool/data/vm-100-disk-0")
	require.NoError(t, err)
	require.NotNil(t, info)

	assert.Equal(t, "rpool/data/vm-100-disk-0", info.Name)
	assert.Equal(t, "10737418240", info.Size)
	assert.Equal(t, "5368709120", info.Used)
	assert.Equal(t, "zfs", info.StorageType)
}

func TestGetVolumeInfo_CorrectArgs(t *testing.T) {
	output := "10737418240\n5368709120\n"

	m := &mockRunner{
		results: []mockResult{
			{output: []byte(output), err: nil},
		},
	}
	b := newTestBackend(m)

	_, err := b.GetVolumeInfo(context.Background(), "rpool/data/vm-100-disk-0")
	require.NoError(t, err)

	require.Len(t, m.calls, 1)
	assert.Equal(t, []string{"get", "-Hp", "-o", "value", "volsize,used", "rpool/data/vm-100-disk-0"}, m.calls[0].Args)
}

func TestGetVolumeInfo_DatasetNotFound(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			{
				output: []byte("cannot open 'rpool/data/vm-999-disk-0': dataset does not exist"),
				err:    fmt.Errorf("exit status 1"),
			},
		},
	}
	b := newTestBackend(m)

	info, err := b.GetVolumeInfo(context.Background(), "rpool/data/vm-999-disk-0")
	require.Error(t, err)
	assert.Nil(t, info)
	assert.Contains(t, err.Error(), "volume rpool/data/vm-999-disk-0 not found")
}

func TestGetVolumeInfo_UnexpectedOutput(t *testing.T) {
	// Only one line instead of the expected two
	m := &mockRunner{
		results: []mockResult{
			{output: []byte("10737418240\n"), err: nil},
		},
	}
	b := newTestBackend(m)

	info, err := b.GetVolumeInfo(context.Background(), "rpool/data/vm-100-disk-0")
	require.Error(t, err)
	assert.Nil(t, info)
	assert.Contains(t, err.Error(), "unexpected zfs get output")
}

func TestGetVolumeInfo_GenericError(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			{
				output: []byte("internal error"),
				err:    fmt.Errorf("exit status 1"),
			},
		},
	}
	b := newTestBackend(m)

	info, err := b.GetVolumeInfo(context.Background(), "rpool/data/vm-100-disk-0")
	require.Error(t, err)
	assert.Nil(t, info)
	assert.Contains(t, err.Error(), "getting volume info")
}

// ---------------------------------------------------------------------------
// PromoteClone
// ---------------------------------------------------------------------------

func TestPromoteClone_Success(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			{output: nil, err: nil},
		},
	}
	b := newTestBackend(m)

	err := b.PromoteClone(context.Background(), "rpool/data/vm-200-disk-0")
	require.NoError(t, err)

	require.Len(t, m.calls, 1)
	assert.Equal(t, []string{"promote", "rpool/data/vm-200-disk-0"}, m.calls[0].Args)
}

func TestPromoteClone_NotAClone(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			{
				output: []byte("cannot promote 'rpool/data/vm-200-disk-0': not a cloned filesystem"),
				err:    fmt.Errorf("exit status 1"),
			},
		},
	}
	b := newTestBackend(m)

	err := b.PromoteClone(context.Background(), "rpool/data/vm-200-disk-0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a clone")
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNew_DefaultRunner(t *testing.T) {
	cfg := &config.Config{
		ZFSTimeout: 5 * time.Second,
	}
	b := New(cfg, nil)
	require.NotNil(t, b)
	assert.Equal(t, 5*time.Second, b.timeout)
	assert.NotNil(t, b.run, "passing nil runner should fall back to DefaultRunner")
}

func TestNew_CustomRunner(t *testing.T) {
	cfg := &config.Config{
		ZFSTimeout: 10 * time.Second,
	}
	called := false
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		called = true
		return nil, nil
	}
	b := New(cfg, runner)
	require.NotNil(t, b)
	assert.Equal(t, 10*time.Second, b.timeout)

	_, _ = b.run(context.Background(), "echo")
	assert.True(t, called, "custom runner should be used")
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

// Compile-time check that *ZFSBackend satisfies storage.StorageBackend.
var _ storage.StorageBackend = (*ZFSBackend)(nil)
