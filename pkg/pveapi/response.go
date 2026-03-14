package pveapi

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes a Proxmox-style JSON response: {"data": ...}
func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"data": data})
}

// WriteError writes a Proxmox-style error response.
func WriteError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"data":   nil,
		"errors": map[string]string{"error": message},
	})
}

// WriteUPID writes a UPID as a Proxmox-style response.
func WriteUPID(w http.ResponseWriter, upid string) {
	WriteJSON(w, http.StatusOK, upid)
}
