package task

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type TaskResult struct {
	UPID       string `json:"upid"`
	Node       string `json:"node"`
	Status     string `json:"status"`
	ExitStatus string `json:"exitstatus,omitempty"`
	Type       string `json:"type"`
	User       string `json:"user"`
	ID         string `json:"id"`
	StartTime  int64  `json:"starttime"`
	EndTime    int64  `json:"endtime,omitempty"`
}
type Store struct {
	mu    sync.Mutex
	tasks map[string]*TaskResult
	path  string
	ttl   time.Duration
	limit int
}

func NewStore() *Store {
	return &Store{tasks: map[string]*TaskResult{}, ttl: 7 * 24 * time.Hour, limit: 10000}
}
func OpenStore(path string) (*Store, error) {
	s := NewStore()
	s.path = path
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		if err = json.Unmarshal(b, &s.tasks); err != nil {
			return nil, fmt.Errorf("task state: %w", err)
		}
	}
	if s.tasks == nil {
		s.tasks = map[string]*TaskResult{}
	}
	for _, r := range s.tasks {
		if r == nil {
			return nil, fmt.Errorf("invalid task state")
		}
		if r.Status == "running" {
			r.Status = "stopped"
			r.ExitStatus = "ERROR: service restarted; retry operation to reconcile storage"
			r.EndTime = time.Now().Unix()
		}
	}
	s.prune()
	if err = s.save(); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Store) prune() {
	cutoff := time.Now().Add(-s.ttl).Unix()
	var keys []string
	for k, r := range s.tasks {
		if r.Status == "running" {
			continue
		}
		if r.StartTime < cutoff {
			delete(s.tasks, k)
		} else {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return s.tasks[keys[i]].StartTime < s.tasks[keys[j]].StartTime })
	for _, k := range keys {
		if len(s.tasks) < s.limit {
			break
		}
		delete(s.tasks, k)
	}
}
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	b, err := json.Marshal(s.tasks)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".tasks-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, s.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (s *Store) Put(r *TaskResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	if _, exists := s.tasks[r.UPID]; !exists && len(s.tasks) >= s.limit {
		return fmt.Errorf("task capacity reached")
	}
	copy := *r
	if copy.StartTime == 0 {
		copy.StartTime = time.Now().Unix()
	}
	previous, existed := s.tasks[r.UPID]
	s.tasks[r.UPID] = &copy
	if err := s.save(); err != nil {
		if existed {
			s.tasks[r.UPID] = previous
		} else {
			delete(s.tasks, r.UPID)
		}
		return err
	}
	return nil
}
func (s *Store) Get(upid string) *TaskResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.tasks[upid]
	if r == nil || (r.Status != "running" && r.StartTime < time.Now().Add(-s.ttl).Unix()) {
		return nil
	}
	copy := *r
	return &copy
}
func (s *Store) IsOurs(upid string) bool { return s.Get(upid) != nil || IsManagedUPID(upid) }
