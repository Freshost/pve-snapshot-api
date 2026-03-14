package task

import "sync"

// TaskResult represents the outcome of a completed task.
type TaskResult struct {
	UPID       string `json:"upid"`
	Node       string `json:"node"`
	Status     string `json:"status"`
	ExitStatus string `json:"exitstatus"`
	Type       string `json:"type"`
	User       string `json:"user"`
	ID         string `json:"id"`
}

// Store is an in-memory store for task results.
type Store struct {
	mu    sync.RWMutex
	tasks map[string]*TaskResult
}

// NewStore creates a new task store.
func NewStore() *Store {
	return &Store{
		tasks: make(map[string]*TaskResult),
	}
}

// Put stores a task result keyed by UPID.
func (s *Store) Put(result *TaskResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[result.UPID] = result
}

// Get returns a task result by UPID, or nil if not found.
func (s *Store) Get(upid string) *TaskResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tasks[upid]
}

// IsOurs returns true if the UPID belongs to a task we created.
func (s *Store) IsOurs(upid string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.tasks[upid]
	return ok
}
