package balance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSecretArgsAndTrim(t *testing.T) {
	var got []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return []byte("sk-abc\n"), nil
	}
	if key := Secret(context.Background(), run, "darwin", "my-service", "my-account"); key != "sk-abc" {
		t.Errorf("key = %q", key)
	}
	want := []string{"security", "find-generic-password", "-s", "my-service", "-a", "my-account", "-w"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v", got)
	}
	fail := func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("exit 44") }
	if key := Secret(context.Background(), fail, "darwin", "x", ""); key != "" {
		t.Errorf("not found must be empty, got %q", key)
	}
}

func TestSecretLinuxUsesSecretTool(t *testing.T) {
	var got []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return []byte("sk-linux\n"), nil
	}
	if key := Secret(context.Background(), run, "linux", "svc", "acct"); key != "sk-linux" {
		t.Errorf("key = %q", key)
	}
	want := []string{"secret-tool", "lookup", "service", "svc", "account", "acct"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v", got)
	}
	got = nil
	Secret(context.Background(), run, "linux", "svc", "")
	if want := []string{"secret-tool", "lookup", "service", "svc"}; !reflect.DeepEqual(got, want) {
		t.Errorf("no account: args = %v", got)
	}
}

func TestSecretUnsupportedOSNeverRuns(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) {
		t.Error("must not run any command on an unsupported OS")
		return nil, nil
	}
	if key := Secret(context.Background(), run, "windows", "x", "y"); key != "" {
		t.Errorf("got %q", key)
	}
}

func TestCacheUsesLookupOnce(t *testing.T) {
	var mu sync.Mutex
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = r.Header.Get("Authorization")
		mu.Unlock()
		w.Write([]byte(sample))
	}))
	defer srv.Close()

	var lookups atomic.Int32
	c := &Cache{Lookup: func(context.Context) string { lookups.Add(1); return "kc-key" }, URL: srv.URL, Client: srv.Client(), Every: time.Millisecond}
	var st State
	for i := 0; i < 100 && !st.Loaded; i++ {
		time.Sleep(20 * time.Millisecond)
		st = c.Get()
	}
	mu.Lock()
	got := auth
	mu.Unlock()
	if !st.Loaded || got != "Bearer kc-key" {
		t.Fatalf("state %+v auth %q", st, got)
	}
	for i := 0; i < 5; i++ { // several refreshes: the keychain is still asked once
		time.Sleep(10 * time.Millisecond)
		c.Get()
	}
	if n := lookups.Load(); n != 1 {
		t.Errorf("keychain asked %d times, want once", n)
	}
}

func TestCacheMissingKeyIsReportedAndNeverCalls(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer srv.Close()
	c := &Cache{Lookup: func(context.Context) string { return "" }, URL: srv.URL, Client: srv.Client(), Every: time.Millisecond}
	for i := 0; i < 5; i++ {
		c.Get()
		time.Sleep(20 * time.Millisecond)
	}
	if st := c.Get(); !st.Missing {
		t.Errorf("no key anywhere must say Missing: %+v", st)
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("a missing key must not call the API, got %d calls", n)
	}
}
