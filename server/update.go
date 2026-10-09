package server

import (
	"sync"

	"github.com/Autumn-27/artex/selfupdate"
)

// One-click updates were removed: this build is a customized fork, and an
// update would overwrite it with an upstream release. The process-restart
// signaling stays because cmd/artex boot/shutdown wiring depends on it.

// restartCh closes once an upgrade is ready or rollback completes; main exits with ExitRestart.
var (
	restartOnce sync.Once
	restartCh   = make(chan struct{})
)

// RestartRequested closes when main should exit and let the supervisor restart it.
func RestartRequested() <-chan struct{} { return restartCh }

func requestRestart() { restartOnce.Do(func() { close(restartCh) }) }

// bootState stores this startup's Bootstrap outcome: pending upgrade, rollback,
// or discarded staging. main supplies it so the UI receives an accurate result.
var (
	bootStateMu sync.Mutex
	bootState   selfupdate.State
)

// SetBootUpdateState is called once by main at startup.
func SetBootUpdateState(st selfupdate.State) {
	bootStateMu.Lock()
	defer bootStateMu.Unlock()
	bootState = st
}

func bootUpdateState() selfupdate.State {
	bootStateMu.Lock()
	defer bootStateMu.Unlock()
	return bootState
}
