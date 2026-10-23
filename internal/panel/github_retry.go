package panel

import (
	"context"
	"io"
	"net/http"
	"time"
)

func githubRouteError(proxy bool, err error) string {
	route := "напрямую"
	if proxy {
		route = "через VPN"
	}
	return route + ": " + err.Error()
}

func githubRequest(ctx context.Context, client *http.Client, raw string) (*http.Response, error) {
	return githubRequestWith(ctx, client, raw, func(ctx context.Context) error {
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	})
}

// Retry one transient server/transport failure per route. The caller's deadline
// also bounds the pause; permission errors and invalid content are not retried.
func githubRequestWith(ctx context.Context, client *http.Client, raw string, wait func(context.Context) error) (*http.Response, error) {
	// Share one route budget across retries, leaving the second route its
	// original time allowance instead of spending both allowances directly.
	cancel := context.CancelFunc(func() {})
	if client.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, client.Timeout)
	}
	finish := func(resp *http.Response, err error) (*http.Response, error) {
		if resp != nil && err == nil {
			resp.Body = &githubResponseBody{resp.Body, cancel}
		} else {
			cancel()
		}
		return resp, err
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return finish(nil, err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return finish(nil, err)
		}
		req.Header.Set("User-Agent", "DARKLS-VPN-Panel/"+version)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := client.Do(req)
		retry := err != nil
		if resp != nil {
			switch resp.StatusCode {
			case 500, 502, 503, 504:
				retry = true
			}
		}
		if !retry || attempt == 1 || ctx.Err() != nil {
			return finish(resp, err)
		}
		if resp != nil {
			resp.Body.Close()
		}
		if err = wait(ctx); err != nil {
			return finish(nil, err)
		}
	}
}

type githubResponseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *githubResponseBody) Close() error { defer b.cancel(); return b.ReadCloser.Close() }
