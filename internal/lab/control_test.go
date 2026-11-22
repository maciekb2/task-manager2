package lab

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestControlRejectsCrossSiteAndRemoteActions(t *testing.T) {
	a := &App{actionToken: "test-session"}
	cases := []struct {
		name, host, origin, fetch, token string
		allowed                          bool
	}{
		{"local", "127.0.0.1:18085", "http://127.0.0.1:18085", "same-origin", "test-session", true},
		{"missing token", "127.0.0.1:18085", "", "", "", false},
		{"foreign origin", "localhost:18085", "https://evil.example", "", "test-session", false},
		{"cross site", "localhost:18085", "", "cross-site", "test-session", false},
		{"remote host", "192.0.2.1:8080", "", "", "test-session", false},
		{"ipv6 loopback", "[::1]:18085", "http://[::1]:18085", "same-origin", "test-session", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://"+c.host+"/api/action", nil)
			r.Host = c.host
			r.Header.Set("Origin", c.origin)
			r.Header.Set("Sec-Fetch-Site", c.fetch)
			r.Header.Set("X-Lab-Token", c.token)
			if got := a.authorize(r) == nil; got != c.allowed {
				t.Fatalf("allowed=%v, want %v", got, c.allowed)
			}
		})
	}
}
func TestTrafficHasResourceAndTimeBounds(t *testing.T) {
	c := DefaultConfig()
	c.RPS = 50
	c.RunUntil = unixSeconds(time.Now().Add(60 * time.Second))
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*Config){func(c *Config) { c.PayloadBytes = -1 }, func(c *Config) { c.PayloadBytes = 8193 }, func(c *Config) { c.RPS = 51 }, func(c *Config) { c.Concurrency = 0 }, func(c *Config) { c.DelayMS = 3001 }, func(c *Config) { c.CPUMS = 51 }, func(c *Config) { c.ErrorPercent = 101 }, func(c *Config) { c.RunUntil = unixSeconds(time.Now().Add(time.Hour)) }, func(c *Config) { c.RunUntil = 0 }} {
		bad := c
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("unbounded traffic configuration accepted")
		}
	}
}
func TestScaleCannotReachInfrastructureOrForeignWorkloads(t *testing.T) {
	k := &Kube{}
	for _, name := range []string{"nats", "control", "observer", "web", "kube-system", "../echo"} {
		if k.scale(name, 1) == nil {
			t.Fatalf("allowed scale of %q", name)
		}
	}
	for _, replicas := range []int{0, 2, 5} {
		if k.scale("generator", replicas) == nil {
			t.Fatal("generator cap bypassed", replicas)
		}
	}
	if k.scale("echo", 6) == nil {
		t.Fatal("replica ceiling bypassed")
	}
}
