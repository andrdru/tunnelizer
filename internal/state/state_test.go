package state_test

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrdru/tunnelizer/internal/state"
)

const testAlias = "db"

func newStore(t *testing.T) *state.Store {
	t.Helper()

	return state.NewStore(t.TempDir())
}

func aliveNone(int) bool { return false }

func aliveAll(int) bool { return true }

func TestStoreWriteRead(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	exp := state.State{
		Alias:     testAlias,
		Status:    state.StatusUp,
		PID:       os.Getpid(),
		SSHPID:    4242,
		StartedAt: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC),
		Restarts:  3,
		LastError: "boom",
	}

	require.NoError(t, store.Write(exp))

	got, err := store.Read(testAlias)
	require.NoError(t, err)
	assert.Equal(t, exp, got)
}

func TestStoreReadFallsBackToPIDFile(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	require.NoError(t, store.WritePID(testAlias, os.Getpid()))

	got, err := store.Read(testAlias)
	require.NoError(t, err)
	assert.Equal(t, state.State{Alias: testAlias, Status: state.StatusUp, PID: os.Getpid()}, got)
}

func TestStoreReadNotFound(t *testing.T) {
	t.Parallel()

	_, err := newStore(t).Read(testAlias)

	require.ErrorIs(t, err, state.ErrStateNotFound)
}

func TestStoreRemove(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	require.NoError(t, store.Write(state.State{Alias: testAlias, Status: state.StatusUp}))
	require.NoError(t, store.WritePID(testAlias, 42))

	require.NoError(t, store.Remove(testAlias))

	_, err := store.Read(testAlias)
	require.ErrorIs(t, err, state.ErrStateNotFound)
	assert.NoError(t, store.Remove(testAlias))
}

func TestStoreList(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	require.NoError(t, store.Write(state.State{Alias: "second", Status: state.StatusUp}))
	require.NoError(t, store.Write(state.State{Alias: "first", Status: state.StatusDown}))

	got, err := store.List()
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "first", got[0].Alias)
	assert.Equal(t, "second", got[1].Alias)

	empty, err := state.NewStore(t.TempDir() + "/absent").List()
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestStorePID(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	require.NoError(t, store.WritePID(testAlias, 4242))

	pid, err := store.ReadPID(testAlias)
	require.NoError(t, err)
	assert.Equal(t, 4242, pid)

	_, err = store.ReadPID("absent")
	require.ErrorIs(t, err, state.ErrStateNotFound)
}

func TestEffectiveStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		st    state.State
		alive func(int) bool
		exp   state.Status
	}{
		{name: "alive keeps stored status", st: state.State{Status: state.StatusUp, PID: 1}, alive: aliveAll, exp: state.StatusUp},
		{
			name: "alive keeps reconnecting", st: state.State{Status: state.StatusReconnecting, PID: 1},
			alive: aliveAll, exp: state.StatusReconnecting,
		},
		{name: "dead pid is stale", st: state.State{Status: state.StatusUp, PID: 1}, alive: aliveNone, exp: state.StatusStale},
		{
			name: "dead pid keeps down", st: state.State{Status: state.StatusDown, PID: 1},
			alive: aliveNone, exp: state.StatusDown,
		},
		{
			name: "dead pid keeps auth_required", st: state.State{Status: state.StatusAuthRequired, PID: 1},
			alive: aliveNone, exp: state.StatusAuthRequired,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(tt *testing.T) {
			tt.Parallel()

			assert.Equal(tt, tc.exp, state.EffectiveStatus(tc.st, tc.alive))
		})
	}
}

func TestPIDAlive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pid  int
		exp  bool
	}{
		{name: "current process", pid: os.Getpid(), exp: true},
		{name: "unused pid", pid: 1 << 22, exp: false},
		{name: "zero pid", pid: 0, exp: false},
		{name: "negative pid", pid: -1, exp: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(tt *testing.T) {
			tt.Parallel()

			assert.Equal(tt, tc.exp, state.PIDAlive(tc.pid))
		})
	}
}

func TestProcessName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pid  int
		exp  string
	}{
		{name: "current process", pid: os.Getpid(), exp: currentComm(t)},
		{name: "unused pid", pid: 1 << 22, exp: ""},
		{name: "zero pid", pid: 0, exp: ""},
		{name: "negative pid", pid: -1, exp: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(tt *testing.T) {
			tt.Parallel()

			assert.Equal(tt, tc.exp, state.ProcessName(tc.pid))
		})
	}
}

func currentComm(t *testing.T) string {
	t.Helper()

	const commMaxLen = 15

	comm := filepath.Base(os.Args[0])

	return comm[:min(len(comm), commMaxLen)]
}

func TestProbe(t *testing.T) {
	t.Parallel()

	t.Run("open port", func(tt *testing.T) {
		tt.Parallel()

		listener, err := newListener(tt)
		require.NoError(tt, err)

		defer func() {
			if cerr := listener.Close(); cerr != nil {
				tt.Errorf("close listener: %v", cerr)
			}
		}()

		assert.True(tt, state.Probe(tt.Context(), listenerPort(tt, listener)))
	})

	t.Run("closed port", func(tt *testing.T) {
		tt.Parallel()

		listener, err := newListener(tt)
		require.NoError(tt, err)

		port := listenerPort(tt, listener)
		require.NoError(tt, listener.Close())

		assert.False(tt, state.Probe(tt.Context(), port))
	})
}

func newListener(t *testing.T) (net.Listener, error) {
	t.Helper()

	var lc net.ListenConfig

	return lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
}

func listenerPort(t *testing.T, listener net.Listener) int {
	t.Helper()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)

	return tcpAddr.Port
}
