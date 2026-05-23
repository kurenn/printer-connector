package util

import (
	"context"
	"time"
)

// Wait blocks for d or until ctx is cancelled, whichever comes first. It
// returns ctx.Err() if the context was cancelled, otherwise nil. Unlike
// time.Sleep it does not keep a loop pinned past a shutdown signal.
func Wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
