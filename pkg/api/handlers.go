package api

import (
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/freshost/pve-snapshot-api/pkg/pveapi"
	"github.com/freshost/pve-snapshot-api/pkg/task"
)

// validVolumeName matches legitimate PVE volume names (e.g. "vm-106-disk-1", "subvol-104-disk-0").
var validVolumeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// handleCopyVolume intercepts POST /api2/json/nodes/{node}/storage/{storage}/content/{volume}.
// For zfspool storage, it performs an instant ZFS snapshot+clone instead of the slow
// Proxmox content copy (zfs send|recv). For non-ZFS storage, it proxies to Proxmox.
func (s *Server) handleCopyVolume(w http.ResponseWriter, r *http.Request) {
	node := r.PathValue("node")
	storageName := r.PathValue("storage")
	volume := r.PathValue("volume")

	// Check storage type — only intercept zfspool
	storageType, err := s.poolResolver.StorageType(r.Context(), storageName)
	if err != nil || storageType != "zfspool" {
		slog.Debug("non-zfs storage, proxying to PVE", "storage", storageName, "type", storageType)
		s.pveProxy.ServeHTTP(w, r)
		return
	}

	// Authenticate
	token := r.Header.Get("Authorization")
	if token == "" {
		pveapi.WriteError(w, http.StatusUnauthorized, "missing Authorization header")
		return
	}
	if err := s.auth.Authenticate(r.Context(), token, storageName); err != nil {
		pveapi.WriteError(w, http.StatusForbidden, err.Error())
		return
	}

	// Cluster routing: if node is not local, forward to our API on that node
	if s.proxy != nil && node != "" && s.proxy.ShouldProxy(r, node) {
		s.proxy.Forward(w, r, node)
		return
	}

	// Parse target volume from form body
	if err := r.ParseForm(); err != nil {
		pveapi.WriteError(w, http.StatusBadRequest, "invalid form body")
		return
	}
	target := r.FormValue("target")
	if target == "" {
		pveapi.WriteError(w, http.StatusBadRequest, "missing target parameter")
		return
	}

	// Strip storage prefix if present (e.g. "local-zfs:vm-100-disk-0" → "vm-100-disk-0")
	targetVol := target
	if idx := strings.Index(target, ":"); idx >= 0 {
		targetVol = target[idx+1:]
	}

	// Validate volume names
	if !validVolumeName.MatchString(volume) {
		pveapi.WriteError(w, http.StatusBadRequest, "invalid volume name")
		return
	}
	if !validVolumeName.MatchString(targetVol) {
		pveapi.WriteError(w, http.StatusBadRequest, "invalid target volume name")
		return
	}

	// Resolve source and target datasets
	sourceDataset, err := s.poolResolver.VolumeToDataset(r.Context(), storageName, volume)
	if err != nil {
		pveapi.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	targetDataset, err := s.poolResolver.VolumeToDataset(r.Context(), storageName, targetVol)
	if err != nil {
		pveapi.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Create snapshot and clone (instant, COW)
	snapName := "csi-" + targetVol
	if err := s.backend.CreateSnapshot(r.Context(), sourceDataset, snapName); err != nil {
		pveapi.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.backend.CloneSnapshot(r.Context(), sourceDataset, snapName, targetDataset); err != nil {
		pveapi.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Generate UPID and store result
	user := task.ExtractUserFromToken(token)
	upid := task.GenerateUPID(node, "imgcopy", volume, user)
	s.taskStore.Put(&task.TaskResult{
		UPID:       upid,
		Node:       node,
		Status:     "stopped",
		ExitStatus: "OK",
		Type:       "imgcopy",
		User:       user,
		ID:         volume,
	})

	slog.Info("volume copied via ZFS snapshot+clone",
		"source", sourceDataset, "target", targetDataset, "upid", upid)

	pveapi.WriteUPID(w, upid)
}

// handleDeleteVolume intercepts DELETE /api2/json/nodes/{node}/storage/{storage}/content/{disk}.
// For zfspool storage, it destroys the ZFS volume directly. For non-ZFS, proxies to Proxmox.
func (s *Server) handleDeleteVolume(w http.ResponseWriter, r *http.Request) {
	node := r.PathValue("node")
	storageName := r.PathValue("storage")
	disk := r.PathValue("disk")

	// Check storage type — only intercept zfspool
	storageType, err := s.poolResolver.StorageType(r.Context(), storageName)
	if err != nil || storageType != "zfspool" {
		s.pveProxy.ServeHTTP(w, r)
		return
	}

	// Authenticate
	token := r.Header.Get("Authorization")
	if token == "" {
		pveapi.WriteError(w, http.StatusUnauthorized, "missing Authorization header")
		return
	}
	if err := s.auth.Authenticate(r.Context(), token, storageName); err != nil {
		pveapi.WriteError(w, http.StatusForbidden, err.Error())
		return
	}

	// Cluster routing
	if s.proxy != nil && node != "" && s.proxy.ShouldProxy(r, node) {
		s.proxy.Forward(w, r, node)
		return
	}

	// Validate volume name
	if !validVolumeName.MatchString(disk) {
		pveapi.WriteError(w, http.StatusBadRequest, "invalid volume name")
		return
	}

	// Resolve dataset
	dataset, err := s.poolResolver.VolumeToDataset(r.Context(), storageName, disk)
	if err != nil {
		pveapi.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Read origin snapshot before destroying the clone
	originSnap, _ := s.backend.GetOriginSnapshot(r.Context(), dataset)

	// Destroy the volume
	if err := s.backend.DestroyVolume(r.Context(), dataset); err != nil {
		pveapi.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Clean up the origin snapshot (e.g. "zroot/VMs/vm-106-disk-1@csi-vm-106-disk-2")
	if originSnap != "" {
		parts := strings.SplitN(originSnap, "@", 2)
		if len(parts) == 2 {
			if err := s.backend.DeleteSnapshot(r.Context(), parts[0], parts[1]); err != nil {
				slog.Warn("failed to clean up origin snapshot", "snapshot", originSnap, "error", err)
			}
		}
	}

	// Generate UPID and store result
	user := task.ExtractUserFromToken(token)
	upid := task.GenerateUPID(node, "imgdel", disk, user)
	s.taskStore.Put(&task.TaskResult{
		UPID:       upid,
		Node:       node,
		Status:     "stopped",
		ExitStatus: "OK",
		Type:       "imgdel",
		User:       user,
		ID:         disk,
	})

	slog.Info("volume destroyed via ZFS", "dataset", dataset, "upid", upid)

	pveapi.WriteUPID(w, upid)
}

// handleTaskStatus handles GET /api2/json/nodes/{node}/tasks/{upid}/status.
// If the UPID belongs to one of our tasks, we return the stored result.
// Otherwise, we proxy to Proxmox.
func (s *Server) handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	upid := r.PathValue("upid")

	if result := s.taskStore.Get(upid); result != nil {
		pveapi.WriteJSON(w, http.StatusOK, result)
		return
	}

	// Not our task — proxy to Proxmox
	s.pveProxy.ServeHTTP(w, r)
}

// handleHealthz returns a simple health check response.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	pveapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleProxy forwards all non-intercepted requests to Proxmox.
func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	s.pveProxy.ServeHTTP(w, r)
}
