// Package routerexec bounds router utility execution, including inherited pipes.
package routerexec

import (
	"context"
	"os"
	"os/exec"
	"time"
)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}
type Real struct{}

func (Real) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Env = append(os.Environ(), "PATH=/usr/sbin:/usr/bin:/sbin:/bin")
	// A child retaining stdout must not keep a timed-out diagnostic blocked.
	c.WaitDelay = time.Second
	return c.CombinedOutput()
}
