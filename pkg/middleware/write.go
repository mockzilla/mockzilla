package middleware

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"
)

const writeTimeout = 500 * time.Millisecond

// safeWrite records history, cache and replay entries. Losing one is preferable
// to touching the response, so the deadline is short and a panic is contained.
// Not for data a caller expects to survive.
func safeWrite(log *slog.Logger, fn func(context.Context)) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("Write failed", "err", r, "stack", string(debug.Stack()))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	fn(ctx)
}
