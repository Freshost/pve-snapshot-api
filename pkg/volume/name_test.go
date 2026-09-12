package volume

import "testing"

func TestValidName(t *testing.T) {
	for _, name := range []string{
		"vm-100-disk-0",
		"vm-100-pvc-11111111-2222-4333-8444-555555555555",
		"vm-100-snapshot-aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
		"vm-200-data_disk.1",
	} {
		if !ValidName(name) {
			t.Errorf("valid VM volume rejected: %q", name)
		}
	}
	for _, name := range []string{
		"", "archive", "vm-0-disk-0", "vm-01-disk-0", "vm-100-", "vm-100-..",
		"subvol-100-disk-0", "base-100-disk-0", "pool:vm-100-disk-0",
		"../vm-100-disk-0", "vm-100-data/child", "vm-100-data@backup",
		"vm-100-data#bookmark", "vm-100-data space", "vm-100-data\n", "vm-100-data;cmd",
	} {
		if ValidName(name) {
			t.Errorf("unsafe or unsupported name accepted: %q", name)
		}
	}
}
