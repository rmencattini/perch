package balance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sample = `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"110.00","granted_balance":"10.00","topped_up_balance":"100.00"}]}`

func TestParse(t *testing.T) {
	got, err := Parse([]byte(sample))
	want := Info{Available: true, Currency: "CNY", Total: "110.00", Granted: "10.00", ToppedUp: "100.00"}
	if err != nil || got != want {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := Parse([]byte(`{"is_available":true,"balance_infos":[]}`)); err == nil {
		t.Error("no currency must be an error")
	}
	if _, err := Parse([]byte(`nope`)); err == nil {
		t.Error("bad json must be an error")
	}
}

func TestFetchSendsBearerAndMapsStatus(t *testing.T) {
	var auth string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(status)
		w.Write([]byte(sample))
	}))
	defer srv.Close()

	info, err := Fetch(context.Background(), srv.Client(), srv.URL, "sk-secret")
	if err != nil || info.Total != "110.00" || auth != "Bearer sk-secret" {
		t.Fatalf("info %+v err %v auth %q", info, err, auth)
	}
	status = http.StatusUnauthorized
	if _, err := Fetch(context.Background(), srv.Client(), srv.URL, "sk-secret"); err == nil || strings.Contains(err.Error(), "sk-secret") {
		t.Errorf("401 must error without the key: %v", err)
	}
	status = http.StatusPaymentRequired
	if _, err := Fetch(context.Background(), srv.Client(), srv.URL, "sk-secret"); err != ErrExhausted {
		t.Errorf("402 = %v, want ErrExhausted", err)
	}
}

func TestCacheNoKeyNeverCallsOut(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer srv.Close()
	c := &Cache{URL: srv.URL, Client: srv.Client(), Every: time.Minute}
	if st := c.Get(); st.Configured || calls != 0 {
		t.Errorf("state %+v, calls %d", st, calls)
	}
}

func TestCacheFetchesOncePerWindow(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Write([]byte(sample))
	}))
	defer srv.Close()
	now := time.Unix(1000, 0)
	c := &Cache{Key: "k", URL: srv.URL, Client: srv.Client(), Every: time.Minute, Now: func() time.Time { return now }}

	if st := c.Get(); !st.Configured || st.Loaded {
		t.Fatalf("first Get returns before the fetch: %+v", st)
	}
	var st State
	for i := 0; i < 100 && !st.Loaded; i++ {
		time.Sleep(20 * time.Millisecond)
		st = c.Get()
	}
	if !st.Loaded || st.Info.Total != "110.00" || calls != 1 {
		t.Fatalf("state %+v, calls %d", st, calls)
	}
	now = now.Add(30 * time.Second)
	c.Get()
	time.Sleep(50 * time.Millisecond)
	if calls != 1 {
		t.Errorf("inside the window: calls = %d", calls)
	}
	now = now.Add(time.Minute)
	c.Get()
	for i := 0; i < 100 && calls < 2; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if calls != 2 {
		t.Errorf("after the window: calls = %d", calls)
	}
}
