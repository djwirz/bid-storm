// Package holdfast adapts the generator to Holdfast (github.com/djwirz/holdfast).
package holdfast

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/djwirz/bid-storm/internal/target"
)

// Adapter implements target.Adapter. Holdfast has no endpoint for creating auctions, so it
// runs Holdfast's own seed script from a checkout, as its load/run.sh does.
type Adapter struct {
	http        *http.Client
	baseURL     string
	checkout    string
	node        string
	databaseURL string
}

var _ target.Adapter = (*Adapter)(nil) // compile-time check that the interface is met

// New refuses a target or database that isn't on this machine.
func New(hc *http.Client, baseURL, checkout, node, databaseURL string) (*Adapter, error) {
	if err := target.CheckLoopback(baseURL); err != nil {
		return nil, fmt.Errorf("target: %w", err)
	}
	// The URL is written to the manifest, so it must not be able to carry anything else.
	if u, err := url.Parse(baseURL); err != nil || u.Scheme != "http" && u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return nil, errors.New("target: want just http://host:port")
	}
	if err := checkDatabase(databaseURL); err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	if _, err := os.Stat(filepath.Join(checkout, "server", "src", "seed-e2e.ts")); err != nil {
		return nil, fmt.Errorf("no Holdfast checkout there (server/src/seed-e2e.ts is missing): %w", err)
	}
	return &Adapter{http: hc, baseURL: strings.TrimRight(baseURL, "/"), checkout: checkout, node: node, databaseURL: databaseURL}, nil
}

// checkDatabase never echoes the URL, which holds a password.
func checkDatabase(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return errors.New("want a postgres:// URL")
	}
	// Query parameters such as ?host= override the address in a connection string.
	if u.RawQuery != "" {
		return errors.New("query parameters are not allowed")
	}
	return target.CheckLoopback(raw)
}

// Name implements target.Adapter.
func (a *Adapter) Name() string { return "holdfast" }

// InstanceHeader is set by Holdfast's local two-instance proxy.
func (a *Adapter) InstanceHeader() string { return "x-holdfast-instance" }

// Preflight refuses a Holdfast that is wired to Stripe: local development can run against
// real test mode, and a storm must never reach Stripe.
func (a *Adapter) Preflight(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/api/config", nil)
	if err != nil {
		return err
	}
	res, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("is the Holdfast stack up? %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GET /api/config: HTTP %d", res.StatusCode)
	}
	// Pointers, so a missing field fails instead of reading as false.
	var cfg struct {
		DevSignIn    *bool `json:"devSignIn"`
		CardRequired *bool `json:"cardRequired"`
	}
	if err := json.NewDecoder(res.Body).Decode(&cfg); err != nil {
		return fmt.Errorf("GET /api/config: %w", err)
	}
	if cfg.DevSignIn == nil || !*cfg.DevSignIn {
		return errors.New("dev sign-in is off; start the stack with load/stack.sh")
	}
	if cfg.CardRequired == nil || *cfg.CardRequired {
		return errors.New("refusing to storm: this Holdfast requires cards, so it is using Stripe, not the fake")
	}
	return nil
}

// CreateAuction runs `node src/seed-e2e.ts <seconds> <title>`.
func (a *Adapter) CreateAuction(ctx context.Context, title string, length time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	seconds := strconv.Itoa(int(math.Ceil(length.Seconds())))
	cmd := exec.CommandContext(ctx, a.node, "src/seed-e2e.ts", seconds, title)
	cmd.Dir = filepath.Join(a.checkout, "server")
	cmd.Env = a.scriptEnv()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("seed-e2e.ts failed (Holdfast needs Node 26): %w\n%s", err, stderr.String())
	}
	return parseSeedOutput(out)
}

// scriptEnv is only what Holdfast's scripts need: no env file is read, so no key from its
// settings is in play.
func (a *Adapter) scriptEnv() []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "DATABASE_URL=" + a.databaseURL}
}

// parseSeedOutput reads the id from the script's last line.
func parseSeedOutput(out []byte) (string, error) {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var auction struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &auction); err != nil || auction.ID == "" {
		return "", errors.New("seed-e2e.ts printed no auction id")
	}
	return auction.ID, nil
}

// Revision reads the checkout's HEAD. --no-optional-locks keeps git from writing to it.
func (a *Adapter) Revision(ctx context.Context) (string, bool, error) {
	git := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", a.checkout}, args...)...)
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	commit, err := git("rev-parse", "HEAD")
	if err != nil {
		return "", false, fmt.Errorf("target commit: %w", err)
	}
	status, err := git("status", "--porcelain")
	if err != nil {
		return "", false, fmt.Errorf("target status: %w", err)
	}
	return commit, status != "", nil
}
