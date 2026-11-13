// Package balance reads the remaining DeepSeek API credit.
package balance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	// KeyEnv is the environment variable that holds the API key. perch never
	// prints it and never writes it to disk.
	KeyEnv = "DEEPSEEK_API_KEY"
	// KeychainEnv and KeychainAccountEnv name the macOS Keychain item (its
	// "service" and "account") to read the key from when KeyEnv is not set.
	KeychainEnv        = "PERCH_DEEPSEEK_KEYCHAIN"
	KeychainAccountEnv = "PERCH_DEEPSEEK_KEYCHAIN_ACCOUNT"
	// The defaults are the item this tool was set up with.
	DefaultKeychainService = "sandboxes-auth"
	DefaultKeychainAccount = "deepseek_api_key"
	// URL is free to call: it does not consume any balance.
	URL = "https://api.deepseek.com/user/balance"
)

// Info is the balance in one currency.
type Info struct {
	Available bool
	Currency  string
	Total     string
	Granted   string
	ToppedUp  string
}

// Parse reads the JSON of GET /user/balance. The first currency wins: the
// account has one in practice.
func Parse(b []byte) (Info, error) {
	var r struct {
		IsAvailable  bool `json:"is_available"`
		BalanceInfos []struct {
			Currency string `json:"currency"`
			Total    string `json:"total_balance"`
			Granted  string `json:"granted_balance"`
			ToppedUp string `json:"topped_up_balance"`
		} `json:"balance_infos"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return Info{}, fmt.Errorf("deepseek balance: parse: %w", err)
	}
	if len(r.BalanceInfos) == 0 {
		return Info{}, fmt.Errorf("deepseek balance: no balance_infos in the response")
	}
	i := r.BalanceInfos[0]
	return Info{Available: r.IsAvailable, Currency: i.Currency, Total: i.Total, Granted: i.Granted, ToppedUp: i.ToppedUp}, nil
}

// Fetch asks DeepSeek for the balance. 401 means the key is missing or
// invalid, 402 that the balance is exhausted. The key never appears in errors.
func Fetch(ctx context.Context, client *http.Client, url, key string) (Info, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Info{}, fmt.Errorf("deepseek balance: bad request")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		return Info{}, fmt.Errorf("deepseek balance: request failed") // err can quote the URL, never the key, but keep it short
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return Info{}, fmt.Errorf("deepseek balance: 401, key missing or invalid")
	case http.StatusPaymentRequired:
		return Info{}, ErrExhausted
	default:
		return Info{}, fmt.Errorf("deepseek balance: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return Info{}, fmt.Errorf("deepseek balance: read failed")
	}
	return Parse(body)
}

// ErrExhausted is the 402 answer: further model calls fail.
var ErrExhausted = fmt.Errorf("deepseek balance: 402, balance exhausted")

// Runner runs a command and returns its stdout. Tests replace it.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner runs the real command.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// Keychain reads a generic password from the macOS Keychain with
// `security find-generic-password -s <service> [-a <account>] -w`. "" means
// not found or access refused. The error text of `security` is dropped on
// purpose.
func Keychain(ctx context.Context, run Runner, service, account string) string {
	args := []string{"find-generic-password", "-s", service}
	if account != "" {
		args = append(args, "-a", account)
	}
	out, err := run(ctx, "security", append(args, "-w")...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Cache fetches in the background at most once per Every, so the one-second
// screen refresh never waits on the network. With neither Key nor Lookup it
// is "not configured": Get reports that and never calls out. Lookup finds the
// key (Keychain) in the background too, since `security` can wait on a prompt.
type Cache struct {
	Key    string
	Lookup func(ctx context.Context) string // used when Key is empty
	URL    string
	Client *http.Client
	Every  time.Duration
	Now    func() time.Time

	mu       sync.Mutex
	missing  bool // Lookup found no key
	info     Info
	err      error
	at       time.Time
	fetching bool
}

// State is what Get hands the screen.
type State struct {
	Configured bool // DEEPSEEK_API_KEY is set
	Loaded     bool // a fetch has finished (ok or not)
	Missing    bool // no key in the environment or the Keychain
	Info       Info
	Err        error
}

func (c *Cache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Get returns the last answer at once and starts a refresh when it is stale.
func (c *Cache) Get() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Key == "" && c.Lookup == nil {
		return State{}
	}
	if c.missing {
		return State{Configured: true, Missing: true}
	}
	if !c.fetching && (c.at.IsZero() || c.now().Sub(c.at) >= c.Every) {
		c.fetching = true
		go c.refresh()
	}
	return State{Configured: true, Loaded: !c.at.IsZero(), Info: c.info, Err: c.err}
}

func (c *Cache) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	url := c.URL
	if url == "" {
		url = URL
	}
	key := c.Key
	if key == "" {
		key = c.Lookup(ctx)
		if key == "" {
			c.mu.Lock()
			c.missing, c.fetching = true, false
			c.mu.Unlock()
			return
		}
		c.mu.Lock()
		c.Key = key // asked once; later refreshes reuse it
		c.mu.Unlock()
	}
	info, err := Fetch(ctx, client, url, key)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.info, c.err, c.at, c.fetching = info, err, c.now(), false
}
