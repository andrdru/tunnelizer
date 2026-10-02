package tunnel_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/andrdru/tunnelizer/internal/config"
	"github.com/andrdru/tunnelizer/internal/state"
	"github.com/andrdru/tunnelizer/internal/tunnel"
	"github.com/andrdru/tunnelizer/internal/tunnel/mocks"
)

const (
	testAlias = "db"
	sshPID    = 4242
)

var (
	errExit   = errors.New("ssh exited")
	errKilled = errors.New("ssh killed")
	errConn   = errors.New("ssh start failed")
)

type testEnv struct {
	ctx         context.Context
	cancel      context.CancelFunc
	ctrl        *gomock.Controller
	starter     *mocks.MockStarter
	master      *mocks.MockMasterChecker
	store       *state.Store
	cfg         config.ResolvedTunnel
	now         time.Time
	sleeps      []time.Duration
	cancelAfter int
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	ctrl := gomock.NewController(t)

	return &testEnv{
		ctx:     ctx,
		cancel:  cancel,
		ctrl:    ctrl,
		starter: mocks.NewMockStarter(ctrl),
		master:  mocks.NewMockMasterChecker(ctrl),
		store:   state.NewStore(t.TempDir()),
		cfg: config.ResolvedTunnel{
			Alias: testAlias, Host: "bastion", User: "deploy", Port: 22,
			LocalPort: 5432, RemoteHost: "db.internal", RemotePort: 5432,
		},
		now: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC),
	}
}

func (te *testEnv) newRunner() *tunnel.Runner {
	return tunnel.NewRunner(te.cfg, te.store, "",
		tunnel.WithStarter(te.starter),
		tunnel.WithMasterChecker(te.master),
		tunnel.WithTimeNowFunc(func() time.Time { return te.now }),
		tunnel.WithSleepFunc(func(ctx context.Context, d time.Duration) bool {
			te.sleeps = append(te.sleeps, d)

			if te.cancelAfter > 0 && len(te.sleeps) >= te.cancelAfter {
				te.cancel()

				return false
			}

			return true
		}),
	)
}

func (te *testEnv) expectStopWhileRunning(proc *mocks.MockProcess, waitCh chan struct{}) {
	te.starter.EXPECT().Start(te.ctx, tunnel.SlaveArgs(te.cfg, "")).Return(proc, nil)
	proc.EXPECT().PID().DoAndReturn(func() int {
		te.cancel()

		return sshPID
	})
	proc.EXPECT().Wait().DoAndReturn(func() error {
		<-waitCh

		return errKilled
	})
	proc.EXPECT().Kill().DoAndReturn(func() error {
		close(waitCh)

		return nil
	})
}

func (te *testEnv) state(t *testing.T) state.State {
	t.Helper()

	st, err := te.store.Read(testAlias)
	require.NoError(t, err)

	return st
}

func TestRunnerStopsWhileRunning(t *testing.T) {
	t.Parallel()

	te := newTestEnv(t)
	te.expectStopWhileRunning(mocks.NewMockProcess(te.ctrl), make(chan struct{}))

	require.NoError(t, te.newRunner().Run(te.ctx))
	assert.Equal(t, state.StatusDown, te.state(t).Status)
	assert.Empty(t, te.sleeps)
}

func TestRunnerReconnectsAfterExit(t *testing.T) {
	t.Parallel()

	te := newTestEnv(t)
	first := mocks.NewMockProcess(te.ctrl)
	second := mocks.NewMockProcess(te.ctrl)

	te.starter.EXPECT().Start(te.ctx, tunnel.SlaveArgs(te.cfg, "")).Return(first, nil)
	first.EXPECT().PID().Return(sshPID)
	first.EXPECT().Wait().Return(errExit)

	te.expectStopWhileRunning(second, make(chan struct{}))

	require.NoError(t, te.newRunner().Run(te.ctx))

	st := te.state(t)
	assert.Equal(t, state.StatusDown, st.Status)
	assert.Equal(t, 1, st.Restarts)
	assert.Equal(t, []time.Duration{time.Second}, te.sleeps)
}

func TestRunnerStartFailureIsRetried(t *testing.T) {
	t.Parallel()

	te := newTestEnv(t)
	proc := mocks.NewMockProcess(te.ctrl)

	te.starter.EXPECT().Start(te.ctx, tunnel.SlaveArgs(te.cfg, "")).Return(nil, errConn)
	te.expectStopWhileRunning(proc, make(chan struct{}))

	require.NoError(t, te.newRunner().Run(te.ctx))

	st := te.state(t)
	assert.Equal(t, state.StatusDown, st.Status)
	assert.Equal(t, 1, st.Restarts)
	assert.Equal(t, []time.Duration{time.Second}, te.sleeps)
}

func TestRunnerBackoffDoublesAndCaps(t *testing.T) {
	t.Parallel()

	const failures = 8

	te := newTestEnv(t)
	te.cancelAfter = failures
	te.starter.EXPECT().Start(te.ctx, tunnel.SlaveArgs(te.cfg, "")).Return(nil, errConn).Times(failures)

	require.NoError(t, te.newRunner().Run(te.ctx))

	st := te.state(t)
	assert.Equal(t, state.StatusDown, st.Status)
	assert.Equal(t, failures, st.Restarts)
	assert.Equal(t, []time.Duration{
		time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
		30 * time.Second,
		30 * time.Second,
	}, te.sleeps)
}

func TestRunnerBackoffResetsAfterStableUptime(t *testing.T) {
	t.Parallel()

	te := newTestEnv(t)
	first := mocks.NewMockProcess(te.ctrl)
	second := mocks.NewMockProcess(te.ctrl)
	third := mocks.NewMockProcess(te.ctrl)
	fourth := mocks.NewMockProcess(te.ctrl)

	te.starter.EXPECT().Start(te.ctx, tunnel.SlaveArgs(te.cfg, "")).Return(first, nil)
	first.EXPECT().PID().Return(sshPID)
	first.EXPECT().Wait().Return(errExit)

	te.starter.EXPECT().Start(te.ctx, tunnel.SlaveArgs(te.cfg, "")).Return(second, nil)
	second.EXPECT().PID().Return(sshPID)
	second.EXPECT().Wait().Return(errExit)

	te.starter.EXPECT().Start(te.ctx, tunnel.SlaveArgs(te.cfg, "")).Return(third, nil)
	third.EXPECT().PID().Return(sshPID)
	third.EXPECT().Wait().DoAndReturn(func() error {
		te.now = te.now.Add(2 * time.Minute)

		return errExit
	})

	te.expectStopWhileRunning(fourth, make(chan struct{}))

	require.NoError(t, te.newRunner().Run(te.ctx))

	st := te.state(t)
	assert.Equal(t, state.StatusDown, st.Status)
	assert.Equal(t, 3, st.Restarts)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, time.Second}, te.sleeps)
}

func TestRunnerInteractiveWithoutMaster(t *testing.T) {
	t.Parallel()

	te := newTestEnv(t)
	te.cfg.Interactive = true
	te.master.EXPECT().Alive(te.ctx, tunnel.CheckArgs(te.cfg, "")).Return(false)

	require.NoError(t, te.newRunner().Run(te.ctx))

	st := te.state(t)
	assert.Equal(t, state.StatusAuthRequired, st.Status)
	assert.Equal(t, "master connection is not alive", st.LastError)
	assert.Empty(t, te.sleeps)
}

func TestRunnerInteractiveWithMaster(t *testing.T) {
	t.Parallel()

	te := newTestEnv(t)
	te.cfg.Interactive = true
	te.master.EXPECT().Alive(te.ctx, tunnel.CheckArgs(te.cfg, "")).Return(true)
	te.expectStopWhileRunning(mocks.NewMockProcess(te.ctrl), make(chan struct{}))

	require.NoError(t, te.newRunner().Run(te.ctx))
	assert.Equal(t, state.StatusDown, te.state(t).Status)
}
