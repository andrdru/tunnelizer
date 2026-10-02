package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var ErrStateNotFound = errors.New("state not found")

const (
	filePerm    = 0o600
	dirPerm     = 0o700
	jsonExt     = ".json"
	pidExt      = ".pid"
	sockExt     = ".sock"
	probeHost   = "127.0.0.1"
	probePeriod = 500 * time.Millisecond
	procDir     = "/proc"
	commFile    = "comm"
)

type Status string

const (
	StatusUp           Status = "up"
	StatusReconnecting Status = "reconnecting"
	StatusAuthRequired Status = "auth_required"
	StatusDown         Status = "down"
	StatusStale        Status = "stale"
)

type State struct {
	Alias     string    `json:"alias"`
	Status    Status    `json:"status"`
	PID       int       `json:"pid"`
	SSHPID    int       `json:"ssh_pid,omitempty"`
	StartedAt time.Time `json:"started_at"`
	Restarts  int       `json:"restarts"`
	LastError string    `json:"last_error,omitempty"`
}

type Store struct {
	dir string
}

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) Dir() string {
	return s.dir
}

func (s *Store) SockPath(alias string) string {
	return filepath.Join(s.dir, alias+sockExt)
}

func (s *Store) Write(st State) error {
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("state.Write: %w", err)
	}

	data, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("state.Write: %w", err)
	}

	path := s.statePath(st.Alias)
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, filePerm); err != nil {
		return fmt.Errorf("state.Write: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("state.Write: %w", err)
	}

	return nil
}

func (s *Store) Read(alias string) (State, error) {
	data, err := os.ReadFile(s.statePath(alias))
	if err == nil {
		var st State

		if uerr := json.Unmarshal(data, &st); uerr != nil {
			return State{}, fmt.Errorf("state.Read %q: %w", alias, uerr)
		}

		return st, nil
	}

	if !errors.Is(err, os.ErrNotExist) {
		return State{}, fmt.Errorf("state.Read %q: %w", alias, err)
	}

	pid, perr := s.ReadPID(alias)
	if perr != nil {
		return State{}, fmt.Errorf("state.Read %q: %w", alias, ErrStateNotFound)
	}

	return State{Alias: alias, Status: StatusUp, PID: pid}, nil
}

func (s *Store) Remove(alias string) error {
	for _, path := range []string{s.statePath(alias), s.pidPath(alias)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("state.Remove %q: %w", alias, err)
		}
	}

	return nil
}

func (s *Store) List() ([]State, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("state.List: %w", err)
	}

	states := make([]State, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), jsonExt) {
			continue
		}

		st, rerr := s.Read(strings.TrimSuffix(entry.Name(), jsonExt))
		if rerr != nil {
			return nil, fmt.Errorf("state.List: %w", rerr)
		}

		states = append(states, st)
	}

	sort.Slice(states, func(i, j int) bool { return states[i].Alias < states[j].Alias })

	return states, nil
}

func (s *Store) WritePID(alias string, pid int) error {
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("state.WritePID: %w", err)
	}

	if err := os.WriteFile(s.pidPath(alias), []byte(strconv.Itoa(pid)), filePerm); err != nil {
		return fmt.Errorf("state.WritePID: %w", err)
	}

	return nil
}

func (s *Store) ReadPID(alias string) (int, error) {
	data, err := os.ReadFile(s.pidPath(alias))
	if err != nil {
		return 0, fmt.Errorf("state.ReadPID %q: %w", alias, ErrStateNotFound)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("state.ReadPID %q: %w", alias, err)
	}

	return pid, nil
}

func (s *Store) statePath(alias string) string {
	return filepath.Join(s.dir, alias+jsonExt)
}

func (s *Store) pidPath(alias string) string {
	return filepath.Join(s.dir, alias+pidExt)
}

func EffectiveStatus(st State, alive func(pid int) bool) Status {
	if alive(st.PID) {
		return st.Status
	}

	switch st.Status {
	case StatusDown, StatusAuthRequired:
		return st.Status
	}

	return StatusStale
}

func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	err := syscall.Kill(pid, 0)

	return err == nil || errors.Is(err, syscall.EPERM)
}

// ProcessName возвращает имя процесса по pid: нужно, чтобы не убить чужой процесс, переиспользовавший pid.
func ProcessName(pid int) string {
	if pid <= 0 {
		return ""
	}

	data, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), commFile))
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

func Probe(ctx context.Context, port int) bool {
	dialer := net.Dialer{Timeout: probePeriod}

	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(probeHost, strconv.Itoa(port)))
	if err != nil {
		return false
	}

	if err := conn.Close(); err != nil {
		return false
	}

	return true
}
