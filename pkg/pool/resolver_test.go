package pool

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockRunner(output string, err error) CommandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(output), err
	}
}

func TestResolve(t *testing.T) {
	r := New(5e9, mockRunner(`{"pool":"rpool/data","type":"zfspool"}`, nil))

	pool, err := r.Resolve(context.Background(), "local-zfs")
	require.NoError(t, err)
	assert.Equal(t, "rpool/data", pool)
}

func TestStorageType(t *testing.T) {
	r := New(5e9, mockRunner(`{"pool":"rpool/data","type":"zfspool"}`, nil))

	typ, err := r.StorageType(context.Background(), "local-zfs")
	require.NoError(t, err)
	assert.Equal(t, "zfspool", typ)
}

func TestVolumeToDataset(t *testing.T) {
	r := New(5e9, mockRunner(`{"pool":"rpool/data","type":"zfspool"}`, nil))

	ds, err := r.VolumeToDataset(context.Background(), "local-zfs", "vm-100-disk-0")
	require.NoError(t, err)
	assert.Equal(t, "rpool/data/vm-100-disk-0", ds)
}

func TestResolverCaching(t *testing.T) {
	callCount := 0
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		callCount++
		return []byte(`{"pool":"tank","type":"zfspool"}`), nil
	}

	r := New(5e9, runner)

	_, err := r.Resolve(context.Background(), "zfs1")
	require.NoError(t, err)
	_, err = r.Resolve(context.Background(), "zfs1")
	require.NoError(t, err)
	_, err = r.StorageType(context.Background(), "zfs1")
	require.NoError(t, err)

	// Should only call pvesh once due to caching
	assert.Equal(t, 1, callCount)
}

func TestResolverError(t *testing.T) {
	r := New(5e9, mockRunner("storage not found", fmt.Errorf("exit 1")))

	_, err := r.Resolve(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "storage not found")
}

func TestResolverInvalidJSON(t *testing.T) {
	r := New(5e9, mockRunner("not json", nil))

	_, err := r.Resolve(context.Background(), "bad")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing storage")
}
