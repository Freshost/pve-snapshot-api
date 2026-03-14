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

func TestDeleteSnapshot_Success_NoClones(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			// list volumes -- no clones reference this snapshot
			{output: []byte("rpool/data/vm-200-disk-0\t-\n"), err: nil},
			// destroy
			{output: nil, err: nil},
		},
	}
	b := newTestBackend(m)

	err := b.DeleteSnapshot(context.Background(), "rpool/data/vm-100-disk-0", "snap1")
	require.NoError(t, err)

	require.Len(t, m.calls, 2)
	// First call: list volumes
	assert.Equal(t, []string{"list", "-H", "-o", "name,pve-snapshot-api:parent", "-t", "volume"}, m.calls[0].Args)
	// Second call: destroy
	assert.Equal(t, []string{"destroy", "rpool/data/vm-100-disk-0@snap1"}, m.calls[1].Args)
}

func TestDeleteSnapshot_PromotesCloneBeforeDestroy(t *testing.T) {
	dataset := "rpool/data/vm-100-disk-0@snap1"
	cloneName := "rpool/data/vm-300-disk-0"

	listOutput := fmt.Sprintf("%s\t%s\nrpool/data/vm-200-disk-0\t-\n", cloneName, dataset)

	m := &mockRunner{
		results: []mockResult{
			// list volumes -- one clone points to our snapshot
			{output: []byte(listOutput), err: nil},
			// promote the clone
			{output: nil, err: nil},
			// destroy
			{output: nil, err: nil},
		},
	}
	b := newTestBackend(m)

	err := b.DeleteSnapshot(context.Background(), "rpool/data/vm-100-disk-0", "snap1")
	require.NoError(t, err)

	require.Len(t, m.calls, 3)
	// promote call
	assert.Equal(t, []string{"promote", cloneName}, m.calls[1].Args)
	// destroy call
	assert.Equal(t, []string{"destroy", dataset}, m.calls[2].Args)
}

func TestDeleteSnapshot_PromotesMultipleClonesBeforeDestroy(t *testing.T) {
	dataset := "rpool/data/vm-100-disk-0@snap1"
	clone1 := "rpool/data/vm-300-disk-0"
	clone2 := "rpool/data/vm-400-disk-0"

	listOutput := fmt.Sprintf("%s\t%s\n%s\t%s\nrpool/data/vm-200-disk-0\t-\n", clone1, dataset, clone2, dataset)

	m := &mockRunner{
		results: []mockResult{
			// list volumes
			{output: []byte(listOutput), err: nil},
			// promote clone1
			{output: nil, err: nil},
			// promote clone2
			{output: nil, err: nil},
			// destroy
			{output: nil, err: nil},
		},
	}
	b := newTestBackend(m)

	err := b.DeleteSnapshot(context.Background(), "rpool/data/vm-100-disk-0", "snap1")
	require.NoError(t, err)

	require.Len(t, m.calls, 4)
	assert.Equal(t, []string{"promote", clone1}, m.calls[1].Args)
	assert.Equal(t, []string{"promote", clone2}, m.calls[2].Args)
	assert.Equal(t, []string{"destroy", dataset}, m.calls[3].Args)
}

func TestDeleteSnapshot_Idempotent_DatasetDoesNotExist(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			// list volumes
			{output: []byte(""), err: nil},
			// destroy -- dataset does not exist
			{
				output: []byte("cannot open 'rpool/data/vm-100-disk-0@snap1': dataset does not exist"),
				err:    fmt.Errorf("exit status 1"),
			},
		},
	}
	b := newTestBackend(m)

	err := b.DeleteSnapshot(context.Background(), "rpool/data/vm-100-disk-0", "snap1")
	require.NoError(t, err, "dataset does not exist should be treated as idempotent success")
}

func TestDeleteSnapshot_Idempotent_CouldNotFindSnapshots(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			// list volumes
			{output: []byte(""), err: nil},
			// destroy -- could not find any snapshots
			{
				output: []byte("could not find any snapshots to destroy"),
				err:    fmt.Errorf("exit status 1"),
			},
		},
	}
	b := newTestBackend(m)

	err := b.DeleteSnapshot(context.Background(), "rpool/data/vm-100-disk-0", "snap1")
	require.NoError(t, err, "could not find any snapshots should be treated as idempotent success")
}

func TestDeleteSnapshot_ListError_NoDatasetsAvailable(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			// list volumes -- no datasets available (not a real error)
			{
				output: []byte("no datasets available"),
				err:    fmt.Errorf("exit status 1"),
			},
			// destroy
			{output: nil, err: nil},
		},
	}
	b := newTestBackend(m)

	err := b.DeleteSnapshot(context.Background(), "rpool/data/vm-100-disk-0", "snap1")
	require.NoError(t, err)
}

func TestDeleteSnapshot_PromoteError(t *testing.T) {
	dataset := "rpool/data/vm-100-disk-0@snap1"
	cloneName := "rpool/data/vm-300-disk-0"

	listOutput := fmt.Sprintf("%s\t%s\n", cloneName, dataset)

	m := &mockRunner{
		results: []mockResult{
			// list volumes
			{output: []byte(listOutput), err: nil},
			// promote fails
			{
				output: []byte("internal error"),
				err:    fmt.Errorf("exit status 1"),
			},
		},
	}
	b := newTestBackend(m)

	err := b.DeleteSnapshot(context.Background(), "rpool/data/vm-100-disk-0", "snap1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "promoting clone")
}

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

	require.Len(t, m.calls, 2)

	// Verify clone command args
	assert.Equal(t, "zfs", m.calls[0].Name)
	assert.Equal(t, []string{"clone", "rpool/data/vm-100-disk-0@snap1", "rpool/data/vm-200-disk-0"}, m.calls[0].Args)

	// Verify set property command args
	assert.Equal(t, "zfs", m.calls[1].Name)
	assert.Equal(t, []string{"set", "pve-snapshot-api:parent=rpool/data/vm-100-disk-0@snap1", "rpool/data/vm-200-disk-0"}, m.calls[1].Args)
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

func TestCloneSnapshot_SetPropertyError(t *testing.T) {
	m := &mockRunner{
		results: []mockResult{
			// clone succeeds
			{output: nil, err: nil},
			// set property fails
			{
				output: []byte("permission denied"),
				err:    fmt.Errorf("exit status 1"),
			},
		},
	}
	b := newTestBackend(m)

	err := b.CloneSnapshot(context.Background(), "rpool/data/vm-100-disk-0", "snap1", "rpool/data/vm-200-disk-0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "setting parent property")
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
	clonesOutput1 := "rpool/data/vm-300-disk-0\trpool/data/vm-100-disk-0@daily\n"
	clonesOutput2 := "rpool/data/vm-400-disk-0\t-\n"

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
