package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"

	"github.com/freshost/pve-snapshot-api/pkg/pool"
	"github.com/freshost/pve-snapshot-api/pkg/pveapi"
	"github.com/freshost/pve-snapshot-api/pkg/task"
)

const maxCopyBody = 16 << 10

// Route before parsing body or volume names: non-local and non-ZFS requests
// must arrive intact. Never fall back on configuration errors.
func (s *Server) prepare(w http.ResponseWriter, r *http.Request) (*pool.Info, string, bool) {
	node := r.PathValue("node")
	if s.proxy != nil && s.proxy.ShouldProxy(r, node) {
		s.proxy.Forward(w, r, node)
		return nil, "", false
	}
	if s.cluster != nil {
		if !s.cluster.IsLocal(node) {
			pveapi.WriteError(w, 400, "wrong destination node")
			return nil, "", false
		}
		node = s.cluster.LocalName()
	}
	info, err := s.poolResolver.Fetch(r.Context(), r.PathValue("storage"))
	if err != nil {
		pveapi.WriteError(w, 502, err.Error())
		return nil, "", false
	}
	if info.Type != "zfspool" {
		s.pveProxy.ServeHTTP(w, r)
		return nil, "", false
	}
	if err = info.Available(node); err != nil {
		pveapi.WriteError(w, 400, err.Error())
		return nil, "", false
	}
	token := r.Header.Get("Authorization")
	if token == "" {
		pveapi.WriteError(w, 401, "missing Authorization header")
		return nil, "", false
	}
	if err = s.auth.Authenticate(r.Context(), token, r.PathValue("storage")); err != nil {
		pveapi.WriteError(w, 403, err.Error())
		return nil, "", false
	}
	return info, node, true
}

func (s *Server) handleCopyVolume(w http.ResponseWriter, r *http.Request) {
	info, node, ok := s.prepare(w, r)
	if !ok {
		return
	}
	source, err := pool.NormalizeVolume(r.PathValue("storage"), r.PathValue("volume"))
	if err != nil {
		pveapi.WriteError(w, 400, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCopyBody)
	var params struct {
		Target     string `json:"target"`
		TargetNode string `json:"target_node"`
	}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct == "application/json" {
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		err = d.Decode(&params)
		if err == nil {
			var extra any
			if e := d.Decode(&extra); e != io.EOF {
				err = fmt.Errorf("unexpected data after JSON object")
			}
		}
	} else if ct == "application/x-www-form-urlencoded" || ct == "" {
		err = r.ParseForm()
		if err == nil {
			for k, v := range r.Form {
				if (k != "target" && k != "target_node") || len(v) != 1 {
					err = fmt.Errorf("invalid copy parameter %s", k)
				}
			}
			params.Target = r.Form.Get("target")
			params.TargetNode = r.Form.Get("target_node")
		}
	} else {
		pveapi.WriteError(w, 415, "unsupported content type")
		return
	}
	if err != nil {
		var limit *http.MaxBytesError
		status := 400
		if errors.As(err, &limit) {
			status = 413
		}
		pveapi.WriteError(w, status, "invalid or oversized copy body")
		return
	}
	if params.Target == "" {
		pveapi.WriteError(w, 400, "missing target parameter")
		return
	}
	if params.TargetNode != "" && params.TargetNode != node && !(params.TargetNode == "localhost") {
		pveapi.WriteError(w, 400, "ZFS copy to a different target_node is unsupported; use native PVE API")
		return
	}
	target, err := pool.NormalizeVolume(r.PathValue("storage"), params.Target)
	if err != nil {
		pveapi.WriteError(w, 400, err.Error())
		return
	}
	if target == source {
		pveapi.WriteError(w, 400, "source and target must differ")
		return
	}
	s.mutate(w, r, info, node, "imgcopy", source, func(ctx context.Context, current *pool.Info) error {
		src, err := current.Dataset(source)
		if err != nil {
			return err
		}
		dst, err := current.Dataset(target)
		if err != nil {
			return err
		}
		return s.backend.CopyVolume(ctx, src, dst)
	})
}

func (s *Server) handleDeleteVolume(w http.ResponseWriter, r *http.Request) {
	info, node, ok := s.prepare(w, r)
	if !ok {
		return
	}
	volume, err := pool.NormalizeVolume(r.PathValue("storage"), r.PathValue("disk"))
	if err != nil {
		pveapi.WriteError(w, 400, err.Error())
		return
	}
	s.mutate(w, r, info, node, "imgdel", volume, func(ctx context.Context, current *pool.Info) error {
		ds, err := current.Dataset(volume)
		if err != nil {
			return err
		}
		return s.backend.DestroyVolume(ctx, ds)
	})
}

// Persist intent before mutation; once accepted, client disconnects do not kill
// a multi-step ZFS operation. Serialize mutations and recheck the storage mapping.
func (s *Server) mutate(w http.ResponseWriter, r *http.Request, initial *pool.Info, node, kind, volume string, fn func(context.Context, *pool.Info) error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if err := r.Context().Err(); err != nil {
		return
	}
	// A request may have waited behind another mutation longer than the ACL TTL.
	if err := s.auth.Authenticate(r.Context(), r.Header.Get("Authorization"), r.PathValue("storage")); err != nil {
		pveapi.WriteError(w, http.StatusForbidden, err.Error())
		return
	}
	current, err := s.poolResolver.Fetch(r.Context(), r.PathValue("storage"))
	if err != nil {
		pveapi.WriteError(w, 502, err.Error())
		return
	}
	if *initial != *current {
		pveapi.WriteError(w, 409, "storage configuration changed; retry")
		return
	}
	user := task.ExtractUserFromToken(r.Header.Get("Authorization"))
	result := &task.TaskResult{UPID: task.GenerateUPID(node, kind, volume, user), Node: node, Status: "running", Type: "psa" + kind, User: user, ID: volume, StartTime: time.Now().Unix()}
	if err = s.taskStore.Put(result); err != nil {
		pveapi.WriteError(w, 503, "cannot persist task")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Minute)
	defer cancel()
	err = fn(ctx, current)
	result.Status = "stopped"
	result.EndTime = time.Now().Unix()
	result.ExitStatus = "OK"
	if err != nil {
		result.ExitStatus = "ERROR: " + err.Error()
	}
	if saveErr := s.taskStore.Put(result); saveErr != nil {
		slog.Error("persist task result", "error", saveErr)
		pveapi.WriteError(w, 503, "cannot persist task result; retry operation")
		return
	}
	// Like PVE, accepted tasks report their result through the status endpoint.
	pveapi.WriteUPID(w, result.UPID)
}

func (s *Server) handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	upid := r.PathValue("upid")
	node := r.PathValue("node")
	if s.proxy != nil && s.proxy.ShouldProxy(r, node) {
		s.proxy.Forward(w, r, node)
		return
	}
	if s.cluster != nil {
		node = s.cluster.LocalName()
	}
	if !s.taskStore.IsOurs(upid) {
		s.pveProxy.ServeHTTP(w, r)
		return
	}
	if task.Node(upid) != node {
		pveapi.WriteError(w, 400, "task node mismatch")
		return
	}
	token := r.Header.Get("Authorization")
	if token == "" {
		pveapi.WriteError(w, 401, "missing Authorization header")
		return
	}
	result := s.taskStore.Get(upid)
	owner := ""
	if result != nil {
		owner = result.User
	}
	if err := s.auth.AuthorizeTask(r.Context(), token, owner, node); err != nil {
		pveapi.WriteError(w, 403, err.Error())
		return
	}
	if result == nil {
		pveapi.WriteError(w, 404, "task expired or unavailable")
		return
	}
	pveapi.WriteJSON(w, 200, result)
}
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	pveapi.WriteJSON(w, 200, map[string]string{"status": "ok"})
}
func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) { s.pveProxy.ServeHTTP(w, r) }
