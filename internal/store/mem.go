package store

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Mem is an in-memory Store used for tests and for running the GUI/preview
// without a database. It is not used in production.
type Mem struct {
	mu       sync.Mutex
	seq      int64
	fileUID  int64
	pipes    map[int64]Pipeline
	files    []ProcessedFile
	users    map[string]User
	sessions map[string]memSession
	clock    func() time.Time
}

type memSession struct {
	userUID int64
	idle    time.Time
	abs     time.Time
}

// NewMem creates an empty in-memory store.
func NewMem() *Mem {
	return &Mem{pipes: map[int64]Pipeline{}, users: map[string]User{}, sessions: map[string]memSession{}, clock: time.Now}
}

func (m *Mem) ListPipelines(_ context.Context) ([]Pipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Pipeline, 0, len(m.pipes))
	for _, p := range m.pipes {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Mem) GetPipeline(_ context.Context, id int64) (Pipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pipes[id]
	if !ok {
		return Pipeline{}, ErrNotFound
	}
	return p, nil
}

func (m *Mem) CreatePipeline(_ context.Context, p Pipeline) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	p.ID = m.seq
	if p.CreatedOn.IsZero() {
		p.CreatedOn = m.clock()
	}
	m.pipes[p.ID] = p
	return p.ID, nil
}

func (m *Mem) SetPipelineEnabled(_ context.Context, id int64, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pipes[id]
	if !ok {
		return ErrNotFound
	}
	p.Enabled = enabled
	m.pipes[id] = p
	return nil
}

func (m *Mem) NextFileUID(_ context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fileUID++
	return m.fileUID, nil
}

func (m *Mem) RecordProcessedFile(_ context.Context, pf ProcessedFile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pf.CollectedOn.IsZero() {
		pf.CollectedOn = m.clock()
	}
	m.files = append(m.files, pf)
	return nil
}

func (m *Mem) ListProcessedFiles(_ context.Context, limit int) ([]ProcessedFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ProcessedFile, len(m.files))
	copy(out, m.files)
	sort.Slice(out, func(i, j int) bool { return out[i].FileUID > out[j].FileUID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Mem) Ping(_ context.Context) error { return nil }
func (m *Mem) Close()                        {}

// --- distributed claim (in-memory: single instance, always grants) ---

func (m *Mem) AlreadyProcessed(_ context.Context, _ int64, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.files {
		if f.Name == name && f.Status == "DONE" {
			return true, nil
		}
	}
	return false, nil
}

func (m *Mem) AlreadyQuarantined(_ context.Context, _ int64, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.files {
		if f.Name == name && f.Status == "QUARANTINED" {
			return true, nil
		}
	}
	return false, nil
}

func (m *Mem) ClaimFile(_ context.Context, _ int64, _ string) (Claim, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	return Claim{UID: m.seq, Fence: 1}, true, nil
}

func (m *Mem) HeartbeatClaim(_ context.Context, _ Claim) (bool, error) { return true, nil }
func (m *Mem) ReleaseClaim(_ context.Context, _ Claim) error           { return nil }

func (m *Mem) CompleteClaimed(ctx context.Context, pf ProcessedFile, _ Claim) (bool, error) {
	if err := m.RecordProcessedFile(ctx, pf); err != nil {
		return false, err
	}
	return true, nil
}

func (m *Mem) QuarantineFile(_ context.Context, _, _ int64, name string, _ int64, _ Claim) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fileUID++
	m.files = append(m.files, ProcessedFile{FileUID: m.fileUID, Name: name, Status: "QUARANTINED", CollectedOn: m.clock()})
	return true, nil
}

func (m *Mem) RecoverProcessedFile(ctx context.Context, pf ProcessedFile) (bool, error) {
	if done, _ := m.AlreadyProcessed(ctx, 0, pf.Name); done {
		return false, nil
	}
	return true, m.RecordProcessedFile(ctx, pf)
}

func (m *Mem) ReconcileOnce(ctx context.Context, fn func(context.Context) error) (bool, error) {
	return true, fn(ctx)
}

// --- users & sessions (in-memory) ---

func (m *Mem) GetUserByUsername(_ context.Context, username string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[username]; ok {
		return u, nil
	}
	return User{}, ErrNotFound
}

func (m *Mem) UpsertAdmin(_ context.Context, username, displayName, email, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.users == nil {
		m.users = map[string]User{}
	}
	m.seq++
	m.users[username] = User{UID: m.seq, Username: username, DisplayName: displayName, Status: "ACTIVE", PasswordHash: passwordHash, Roles: []string{"Administrator"}}
	return nil
}

func (m *Mem) CreateSession(_ context.Context, userUID int64, tokenHash []byte, idleExp, absExp time.Time, addr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions == nil {
		m.sessions = map[string]memSession{}
	}
	m.sessions[string(tokenHash)] = memSession{userUID: userUID, idle: idleExp, abs: absExp}
	return nil
}

func (m *Mem) SessionUser(_ context.Context, tokenHash []byte) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[string(tokenHash)]
	if !ok || m.clock().After(s.idle) || m.clock().After(s.abs) {
		return User{}, ErrNotFound
	}
	for _, u := range m.users {
		if u.UID == s.userUID {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

func (m *Mem) TouchSession(_ context.Context, tokenHash []byte, idleExp time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[string(tokenHash)]; ok {
		s.idle = idleExp
		m.sessions[string(tokenHash)] = s
	}
	return nil
}

func (m *Mem) RevokeSession(_ context.Context, tokenHash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, string(tokenHash))
	return nil
}
