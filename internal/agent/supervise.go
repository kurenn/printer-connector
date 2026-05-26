package agent

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"
)

// loopRestartDelay is the pause before a supervised loop is restarted after it
// panics or returns unexpectedly. Long enough to avoid a hot crash-loop, short
// enough that telemetry resumes quickly once the fault clears.
const loopRestartDelay = 2 * time.Second

// superviseLoop runs fn until ctx is cancelled. A loop body is only supposed to
// return when ctx is done; any other outcome — a returned error or a panic — is
// a fault we recover from and restart, rather than letting the goroutine vanish.
//
// This is the core guarantee against silent death: without it, an unrecovered
// panic in any one loop (e.g. parsing a malformed Moonraker payload) unwinds its
// goroutine and crashes the whole process, taking heartbeat, commands and
// snapshots down together with no surviving log line.
func (a *Agent) superviseLoop(ctx context.Context, name string, fn func(context.Context) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := a.runGuarded(name, fn, ctx)

		// ctx cancellation is the one clean exit — propagate it and stop.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err == nil {
			a.log.Error("loop exited without an error and without shutdown, restarting", "loop", name)
		} else {
			a.log.Error("loop stopped, restarting", "loop", name, "error", err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(a.restartDelay):
		}
	}
}

// runGuarded runs fn and converts a panic into an error so superviseLoop can
// restart the loop instead of the panic crashing the process. The stack is
// logged at Error so a recovered panic is never silent.
func (a *Agent) runGuarded(name string, fn func(context.Context) error, ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			a.log.Error("loop panicked, recovered",
				"loop", name,
				"panic", r,
				"stack", string(debug.Stack()),
			)
			err = fmt.Errorf("panic in %s loop: %v", name, r)
		}
	}()
	return fn(ctx)
}

// snapshotStalled reports whether telemetry has gone quiet: no snapshot batch
// has been successfully pushed within threshold. Pure so the watchdog's
// decision is unit-testable without goroutine timing.
func snapshotStalled(lastPush, now time.Time, threshold time.Duration) bool {
	return now.Sub(lastPush) > threshold
}
