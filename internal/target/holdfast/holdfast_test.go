package holdfast

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/djwirz/bid-storm/internal/target"
)

const localDB = "postgres://user:placeholder@localhost:5432/db"

func checkout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "server", "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "seed-e2e.ts"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestOnlyThisMachine(t *testing.T) {
	for _, ok := range []string{"http://localhost:3000", "http://127.0.0.1:3000", "http://[::1]:3000"} {
		if err := target.CheckLoopback(ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"https://api.stripe.com", "https://holdfast.example.com", "http://192.168.1.5:3000",
		"http://localhost.example.com", "http://localhost@example.com", "localhost:3000", ""} {
		if target.CheckLoopback(bad) == nil {
			t.Errorf("%q allowed", bad)
		}
	}
	dir := checkout(t)
	for _, base := range []string{"http://user:secret@localhost:3000", "http://localhost:3000/?token=x", "http://example.com"} {
		if _, err := New(http.DefaultClient, base, dir, "node", localDB); err == nil {
			t.Errorf("target %s allowed", base)
		}
	}
	for _, db := range []string{
		"postgres://u:p@db.example.com/db",
		"postgres://u:p@localhost/db?host=db.example.com",
		"mysql://u:p@localhost/db",
	} {
		if _, err := New(http.DefaultClient, "http://localhost:3000", dir, "node", db); err == nil {
			t.Errorf("database %s allowed", db)
		}
	}
	if _, err := New(http.DefaultClient, "http://localhost:3000", dir, "node", localDB); err != nil {
		t.Errorf("local setup refused: %v", err)
	}
}

func TestPreflight(t *testing.T) {
	for name, tc := range map[string]struct {
		config string
		ok     bool
	}{
		"fake payments": {`{"devSignIn":true,"cardRequired":false}`, true},
		"real Stripe":   {`{"devSignIn":true,"cardRequired":true}`, false},
		"no dev login":  {`{"devSignIn":false,"cardRequired":false}`, false},
		"unknown shape": {`{}`, false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			io.WriteString(w, tc.config)
		}))
		a, err := New(server.Client(), server.URL, checkout(t), "node", localDB)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Preflight(context.Background()); (err == nil) != tc.ok {
			t.Errorf("%s: %v", name, err)
		}
		server.Close()
	}
}

func TestParseSeedOutput(t *testing.T) {
	id, err := parseSeedOutput([]byte("a warning\n{\"id\":\"abc\",\"title\":\"t\"}\n"))
	if err != nil || id != "abc" {
		t.Fatalf("%q, %v", id, err)
	}
	if _, err := parseSeedOutput([]byte("nope")); err == nil {
		t.Fatal("no error")
	}
}
