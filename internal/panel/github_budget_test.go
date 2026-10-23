package panel

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type budgetTransport func(*http.Request) (*http.Response, error)

func (f budgetTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGitHubRetrySharesDeadlineAndKeepsBodyReadable(t *testing.T) {
	var contexts []context.Context
	client := &http.Client{Timeout: time.Second, Transport: budgetTransport(func(r *http.Request) (*http.Response, error) {
		contexts = append(contexts, r.Context())
		code := 200
		if len(contexts) == 1 {
			code = 503
		}
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader("verified bytes")), Header: make(http.Header), Request: r}, nil
	})}
	resp, err := githubRequestWith(context.Background(), client, "https://github.com/example", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(contexts) != 2 {
		t.Fatal("missing retry")
	}
	a, _ := contexts[0].Deadline()
	b, _ := contexts[1].Deadline()
	if !a.Equal(b) {
		t.Fatal("retry received a new time budget")
	}
	if contexts[1].Err() != nil {
		t.Fatal("response canceled before body read")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "verified bytes" {
		t.Fatal("body not readable", err)
	}
	resp.Body.Close()
	if contexts[1].Err() != context.Canceled {
		t.Fatal("route context not released on body close")
	}
}
