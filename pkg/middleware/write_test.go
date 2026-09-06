package middleware

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSafeWrite(t *testing.T) {
	tests := []struct {
		name         string
		fn           func(*testing.T, context.Context)
		wantPanicLog bool
	}{
		{
			name: "Write receives a live context carrying the write deadline",
			fn: func(t *testing.T, ctx context.Context) {
				t.Helper()
				deadline, ok := ctx.Deadline()
				assert.True(t, ok)
				assert.WithinDuration(t, time.Now().Add(writeTimeout), deadline, time.Second)
				assert.NoError(t, ctx.Err())
			},
		},
		{
			name:         "Panicking driver is contained and logged",
			fn:           func(*testing.T, context.Context) { panic("driver exploded") },
			wantPanicLog: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var logged bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelError}))

			ran := false
			assert.NotPanics(t, func() {
				safeWrite(log, func(ctx context.Context) {
					ran = true
					tc.fn(t, ctx)
				})
			})

			assert.True(t, ran)

			if !tc.wantPanicLog {
				assert.Empty(t, logged.String())
				return
			}

			assert.Contains(t, logged.String(), "Write failed")
			assert.Contains(t, logged.String(), "driver exploded")
			assert.Contains(t, logged.String(), "stack=")
		})
	}
}
