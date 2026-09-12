// Package volume validates PVE VM block-volume names without accepting paths.
package volume

import "regexp"

// PVE permits named VM volumes, including CSI pvc/snapshot identifiers.
// Keep the suffix restricted to safe ZFS component characters.
var namePattern = regexp.MustCompile(`^vm-[1-9][0-9]*-[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidName reports whether name is a supported VM block-volume component.
func ValidName(name string) bool {
	return namePattern.MatchString(name)
}
