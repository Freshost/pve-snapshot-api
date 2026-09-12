package zfs

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/freshost/pve-snapshot-api/pkg/config"
	"github.com/stretchr/testify/require"
)

// Model native snapshot ownership and origin changes, rather than supplying a
// canned success for each expected command. The same lifecycle runs on real ZFS
// in integration_test.go.
type modelSnapshot struct {
	seq      int
	deferred bool
}
type zfsModel struct {
	volumes        map[string]dataset
	snaps          map[string]modelSnapshot
	seq            int
	ambiguousClone bool
	failClone      bool
	failList       bool
	commands       []string
}

func newModel() *zfsModel {
	return &zfsModel{volumes: map[string]dataset{}, snaps: map[string]modelSnapshot{}}
}
func (m *zfsModel) add(name string) {
	m.seq++
	m.volumes[name] = dataset{name: name, kind: "volume", origin: "-", guid: fmt.Sprint(m.seq), sourceGUID: "-"}
}
func (m *zfsModel) collect() {
	for s, v := range m.snaps {
		if v.deferred && !m.referenced(s) {
			delete(m.snaps, s)
		}
	}
}
func (m *zfsModel) referenced(s string) bool {
	for _, v := range m.volumes {
		if v.origin == s {
			return true
		}
	}
	return false
}
func (m *zfsModel) run(_ context.Context, name string, args ...string) ([]byte, error) {
	m.commands = append(m.commands, strings.Join(args, " "))
	fail := func(s string) ([]byte, error) { return []byte(s), fmt.Errorf("exit status 1") }
	last := args[len(args)-1]
	switch args[0] {
	case "list":
		if m.failList {
			return fail("I/O error")
		}
		if strings.Contains(strings.Join(args, " "), "name,type,origin,guid") {
			var lines []string
			for _, v := range m.volumes {
				lines = append(lines, strings.Join([]string{v.name, v.kind, v.origin, v.guid, v.sourceGUID}, "\t"))
			}
			sort.Strings(lines)
			return []byte(strings.Join(lines, "\n")), nil
		}
		var lines []string
		for s := range m.snaps {
			if strings.HasPrefix(s, last+"@") {
				if strings.Contains(strings.Join(args, " "), "name,pve-snapshot-api:managed") {
					lines = append(lines, s+"\t1")
				} else {
					lines = append(lines, s)
				}
			}
		}
		sort.Strings(lines)
		return []byte(strings.Join(lines, "\n")), nil
	case "snapshot":
		owner, _, _ := strings.Cut(last, "@")
		if _, ok := m.volumes[owner]; !ok {
			return fail("source missing")
		}
		if _, ok := m.snaps[last]; ok {
			return fail("snapshot exists")
		}
		m.seq++
		m.snaps[last] = modelSnapshot{seq: m.seq}
		return nil, nil
	case "clone":
		if m.failClone {
			return fail("clone failed")
		}
		origin := args[len(args)-2]
		if _, ok := m.snaps[origin]; !ok {
			return fail("snapshot missing")
		}
		if _, ok := m.volumes[last]; ok {
			return fail("target exists")
		}
		m.add(last)
		v := m.volumes[last]
		v.origin = origin
		for _, a := range args {
			if strings.HasPrefix(a, "pve-snapshot-api:source-guid=") {
				v.sourceGUID = strings.TrimPrefix(a, "pve-snapshot-api:source-guid=")
			}
		}
		m.volumes[last] = v
		if m.ambiguousClone {
			m.ambiguousClone = false
			return fail("transport interrupted after clone")
		}
		return nil, nil
	case "promote":
		child := m.volumes[last]
		owner, _, ok := strings.Cut(child.origin, "@")
		if !ok {
			return fail("not a clone")
		}
		cut, ok := m.snaps[child.origin]
		if !ok {
			return fail("origin missing")
		}
		parent := m.volumes[owner]
		newParentOrigin := last + "@" + strings.SplitN(child.origin, "@", 2)[1]
		moved := map[string]string{}
		for snap, s := range m.snaps {
			if strings.HasPrefix(snap, owner+"@") && s.seq <= cut.seq {
				newName := last + "@" + strings.SplitN(snap, "@", 2)[1]
				if _, exists := m.snaps[newName]; exists {
					return fail("snapshot conflict")
				}
				moved[snap] = newName
			}
		}
		for old, newName := range moved {
			m.snaps[newName] = m.snaps[old]
			delete(m.snaps, old)
		}
		for n, v := range m.volumes {
			if newName, ok := moved[v.origin]; ok {
				v.origin = newName
				m.volumes[n] = v
			}
		}
		child.origin = parent.origin
		parent.origin = newParentOrigin
		m.volumes[last] = child
		m.volumes[owner] = parent
		m.collect()
		return nil, nil
	case "destroy":
		for _, a := range args {
			if a == "-r" || a == "-R" {
				return fail("recursive deletion forbidden")
			}
		}
		if snap, ok := m.snaps[last]; ok {
			if len(args) > 2 && args[1] == "-d" {
				snap.deferred = true
				m.snaps[last] = snap
				m.collect()
				return nil, nil
			}
			if m.referenced(last) {
				return fail("dependent clones")
			}
			delete(m.snaps, last)
			return nil, nil
		}
		if strings.Contains(last, "@") {
			return fail("snapshot missing")
		}
		for s := range m.snaps {
			if strings.HasPrefix(s, last+"@") {
				return fail("has snapshots")
			}
		}
		delete(m.volumes, last)
		m.collect()
		return nil, nil
	}
	return fail("unsupported model command " + name + " " + strings.Join(args, " "))
}
func permutations(a []int) [][]int {
	if len(a) == 0 {
		return [][]int{{}}
	}
	var out [][]int
	for i, v := range a {
		rest := append([]int{}, a[:i]...)
		rest = append(rest, a[i+1:]...)
		for _, p := range permutations(rest) {
			out = append(out, append([]int{v}, p...))
		}
	}
	return out
}
func TestCloneLifecycleAllDeletionOrders(t *testing.T) {
	for _, suffix := range []string{"disk-0", "pvc-11111111-2222-4333-8444-555555555555"} {
		names := []string{"tank/data/vm-100-" + suffix, "tank/data/vm-200-" + suffix, "tank/data/vm-300-" + suffix, "tank/data/vm-400-" + suffix}
		for _, order := range permutations([]int{0, 1, 2, 3}) {
			t.Run(suffix+fmt.Sprint(order), func(t *testing.T) {
				m := newModel()
				m.add(names[0])
				z := New(&config.Config{ZFSTimeout: time.Second}, m.run)
				require.NoError(t, z.CopyVolume(context.Background(), names[0], names[1]))
				require.NoError(t, z.CopyVolume(context.Background(), names[0], names[2]))
				require.NoError(t, z.CopyVolume(context.Background(), names[1], names[3]))
				for _, i := range order {
					require.NoError(t, z.DestroyVolume(context.Background(), names[i]), m.commands)
					require.NotContains(t, m.volumes, names[i])
					for _, v := range m.volumes {
						if v.origin != "-" {
							require.Contains(t, m.snaps, v.origin, "live clone must retain its origin")
						}
					}
				}
				require.Empty(t, m.volumes)
				require.Empty(t, m.snaps)
			})
		}
	}
}
func TestCopyRetryAndFailureCleanup(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		t.Run(fmt.Sprint(ambiguous), func(t *testing.T) {
			m := newModel()
			src, dst := "tank/data/vm-100-disk-0", "tank/data/vm-200-disk-0"
			m.add(src)
			z := New(&config.Config{ZFSTimeout: time.Second}, m.run)
			if ambiguous {
				m.ambiguousClone = true
			} else {
				m.failClone = true
			}
			require.Error(t, z.CopyVolume(context.Background(), src, dst))
			m.failClone = false
			if !ambiguous {
				require.Empty(t, m.snaps)
			}
			require.NoError(t, z.CopyVolume(context.Background(), src, dst))
			require.NoError(t, z.CopyVolume(context.Background(), src, dst))
			require.Len(t, m.volumes, 2)
			require.Len(t, m.snaps, 1)
			require.NoError(t, z.DestroyVolume(context.Background(), dst))
			require.Empty(t, m.snaps)
		})
	}
}
func TestRefuseFilesystemAndUnknownTarget(t *testing.T) {
	m := newModel()
	src, dst := "tank/data/vm-100-disk-0", "tank/data/vm-200-disk-0"
	m.add(src)
	m.add(dst)
	z := New(&config.Config{ZFSTimeout: time.Second}, m.run)
	require.Error(t, z.CopyVolume(context.Background(), src, dst))
	require.Empty(t, m.snaps)
	v := m.volumes[src]
	v.kind = "filesystem"
	m.volumes[src] = v
	require.Error(t, z.DestroyVolume(context.Background(), src))
	require.Contains(t, m.volumes, src)
	require.Error(t, z.DestroyVolume(context.Background(), "tank/data/archive"))
	m.failList = true
	require.Error(t, z.DestroyVolume(context.Background(), dst))
	require.Contains(t, m.volumes, dst)
}

func TestReconcileSnapshotLeftByCrash(t *testing.T) {
	m := newModel()
	src, dst := "tank/data/vm-100-disk-0", "tank/data/vm-200-disk-0"
	m.add(src)
	m.snaps[src+"@psa-interrupted"] = modelSnapshot{seq: 2}
	m.snaps[src+"@user-backup"] = modelSnapshot{seq: 3}
	z := New(&config.Config{ZFSTimeout: time.Second}, m.run)
	require.NoError(t, z.CopyVolume(context.Background(), src, dst))
	require.NotContains(t, m.snaps, src+"@psa-interrupted")
	require.Contains(t, m.snaps, src+"@user-backup")
}
