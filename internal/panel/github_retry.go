package panel

import (
	"context"
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
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, err
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
			return resp, err
		}
		if resp != nil {
			resp.Body.Close()
		}
		if err = wait(ctx); err != nil {
			return nil, err
		}
	}
}
