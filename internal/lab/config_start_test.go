package lab

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type configTransport func(*http.Request) (*http.Response, error)

func (f configTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestInitialConfigRetriesInsteadOfServingDefaults(t *testing.T) {
	var calls atomic.Int32
	a := &App{role: "echo", config: DefaultConfig(), http: &http.Client{Transport: configTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "http://control:8080/internal/config" {
			t.Error("unexpected target", r.URL)
		}
		status, body := 200, `{"concurrency":1,"delay_ms":800,"error_percent":100}`
		if calls.Add(1) == 1 {
			status = 503
			body = `{}`
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.loadInitialConfig(ctx); err != nil {
		t.Fatal(err)
	}
	if a.config.ErrorPercent != 100 || a.config.DelayMS != 800 || calls.Load() != 2 {
		t.Fatal("default configuration escaped initialization", a.config, calls.Load())
	}
}
