package task

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// Synthetic UPIDs use a distinct worker type and 64 random bits, never a real
// worker PID. They must only be polled through this service.
func GenerateUPID(node, kind, id, user string) string {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		panic(err)
	}
	return fmt.Sprintf("UPID:%s:%08X:%08X:%08X:psa%s:%s:%s:", node, binary.BigEndian.Uint32(nonce[:4]), binary.BigEndian.Uint32(nonce[4:]), time.Now().Unix(), kind, id, user)
}
func IsManagedUPID(upid string) bool {
	p := strings.Split(upid, ":")
	return len(p) == 9 && p[0] == "UPID" && (p[5] == "psaimgcopy" || p[5] == "psaimgdel")
}
func Node(upid string) string {
	p := strings.Split(upid, ":")
	if len(p) != 9 || p[0] != "UPID" {
		return ""
	}
	return p[1]
}
func ExtractUserFromToken(token string) string {
	v := strings.TrimPrefix(token, "PVEAPIToken=")
	id, _, _ := strings.Cut(v, "=")
	return id
}
