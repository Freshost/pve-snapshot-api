package task

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// GenerateUPID creates a Proxmox-compatible UPID string.
// Format: UPID:{node}:{pid}:{pstart}:{starttime}:{type}:{id}:{user}:
func GenerateUPID(node, taskType, id, user string) string {
	pid := os.Getpid()
	now := time.Now().Unix()
	return fmt.Sprintf("UPID:%s:%08X:%08X:%08X:%s:%s:%s:",
		node, pid, pid, now, taskType, id, user)
}

// ExtractUserFromToken extracts the user@realm part from a PVE API token.
// Token format: PVEAPIToken=user@realm!tokenid=secret
func ExtractUserFromToken(token string) string {
	token = strings.TrimPrefix(token, "PVEAPIToken=")
	// user@realm!tokenid=secret
	parts := strings.SplitN(token, "!", 2)
	if len(parts) < 1 {
		return "root@pam"
	}
	return parts[0]
}
