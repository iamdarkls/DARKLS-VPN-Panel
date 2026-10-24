package routerexec

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCommandCancellationIsBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := (Real{}).Run(ctx, "sh", "-c", "exec sleep 5")
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("command ignored diagnostic deadline")
	}
}
