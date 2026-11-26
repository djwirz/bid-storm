package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djwirz/bid-storm/internal/record"
)

func TestPassesThroughAndRecords(t *testing.T) {
	replies := []struct {
		status int
		body   string
	}{
		{503, `{"error":"transient","message":"PRIVATE-MESSAGE"}`},
		{200, `{"ok":true,"paymentIntentId":"pi_1","status":"requires_capture","amountCapturable":1100}`},
		{200, `{"ok":false,"reason":"rate_limited","retryable":true}`},
		{200, `{"ok":true}`},
		{200, `{"intents":[]}`},
	}
	var got []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		reply := replies[len(got)]
		got = append(got, r.Method+" "+r.URL.RequestURI()+" "+string(b))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Fake", "yes")
		w.WriteHeader(reply.status)
		io.WriteString(w, reply.body)
	}))
	defer fake.Close()
	p, err := New(fake.URL)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "events.jsonl")
	w, err := record.NewWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	p.Record(w, record.NewClock(), "a1")
	server := httptest.NewServer(p)
	defer server.Close()

	authorize := `{"idempotencyKey":"b1","userId":"PRIVATE-USER","amount":1100,"metadata":{"bidId":"b1","auctionId":"a1"}}`
	cancel := `{"paymentIntentId":"pi_1","idempotencyKey":"cancel_hold:b1:1"}`
	call := func(i int, method, path, body string, outstanding int) {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != replies[i].status || string(b) != replies[i].body || res.Header.Get("X-Fake") != "yes" {
			t.Fatalf("call %d changed on the way back: %d %s", i, res.StatusCode, b)
		}
		if got[i] != method+" "+path+" "+body {
			t.Fatalf("call %d changed on the way in: %s", i, got[i])
		}
		if n := p.Outstanding(); n != outstanding {
			t.Fatalf("after call %d: %d outstanding, want %d", i, n, outstanding)
		}
	}
	call(0, "POST", "/authorize", authorize, 1) // unknown outcome
	call(1, "POST", "/authorize", authorize, 1) // held
	call(2, "POST", "/cancel", cancel, 1)       // retryable failure
	call(3, "POST", "/cancel", cancel, 0)       // released
	call(4, "GET", "/audit", "", 0)             // not a hold: not recorded

	held, calls := p.Held()
	if !held["b1"] || calls["authorize"] != 2 || calls["cancel"] != 2 {
		t.Fatalf("held %v calls %v", held, calls)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 4 {
		t.Fatalf("%d events", len(lines))
	}
	for _, want := range []string{`"op":"cancel"`, `"bid_id":"b1"`, `"auction_id":"a1"`, `"intent_id":"pi_1"`, `"ok":true`} {
		if !strings.Contains(lines[3], want) {
			t.Errorf("cancel event lacks %s: %s", want, lines[3])
		}
	}
	if strings.Contains(string(raw), "PRIVATE") {
		t.Fatal("a user id or message text was recorded")
	}
}

func TestDeadFakeLooksTransient(t *testing.T) {
	fake := httptest.NewServer(http.NotFoundHandler())
	fake.Close()
	p, err := New(fake.URL)
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	p.ServeHTTP(res, httptest.NewRequest("POST", "/authorize", strings.NewReader(`{}`)))
	if res.Code != http.StatusBadGateway {
		t.Fatalf("HTTP %d", res.Code)
	}
}

func TestRemoteFakeRefused(t *testing.T) {
	if _, err := New("https://api.stripe.com"); err == nil {
		t.Fatal("allowed")
	}
}
