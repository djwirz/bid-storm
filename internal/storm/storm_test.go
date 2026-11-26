package storm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/djwirz/bid-storm/internal/contract"
	"github.com/djwirz/bid-storm/internal/record"
	"github.com/djwirz/bid-storm/internal/schedule"
)

const token = "SESSIONTOKEN-must-not-be-recorded"

// fakeTarget speaks just enough of the contract. bid decides each bid's reply.
type fakeTarget struct {
	bid func(w http.ResponseWriter, key string, amount int64)

	mu      sync.Mutex
	arrived []time.Time
	bad     []string // contract mistakes by the client
}

func (f *fakeTarget) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	complain := func(msg string) {
		f.mu.Lock()
		f.bad = append(f.bad, msg)
		f.mu.Unlock()
	}
	switch {
	case r.URL.Path == "/api/auth/dev":
		http.SetCookie(w, &http.Cookie{Name: "holdfast_session", Value: token})
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/auctions/"):
		io.WriteString(w, `{"auction":{"status":"open","minIncrement":100,"minimumNextBid":1000}}`)
	case strings.HasPrefix(r.URL.Path, "/api/bids/"):
		fmt.Fprintf(w, `{"bid":{"id":%q,"status":"accepted","reason":null}}`, strings.TrimPrefix(r.URL.Path, "/api/bids/"))
	case r.Method == http.MethodPost:
		f.mu.Lock()
		f.arrived = append(f.arrived, time.Now())
		f.mu.Unlock()
		if r.Header.Get("Origin") != "http://origin.test" {
			complain("origin " + r.Header.Get("Origin"))
		}
		if c, err := r.Cookie("holdfast_session"); err != nil || c.Value != token {
			complain("no session")
		}
		var body struct{ Amount int64 }
		json.NewDecoder(r.Body).Decode(&body)
		f.bid(w, r.Header.Get("Idempotency-Key"), body.Amount)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func accept(w http.ResponseWriter, key string, _ int64) {
	w.Header().Set("x-instance", "3001")
	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, `{"bid":{"id":"id-%s","status":"accepted","reason":null}}`, key)
}

type recording struct {
	res       record.Results
	instances []string
	err       error
	raw       string
	bids      map[int]record.Bid
	polls     []record.Poll
}

func storm(t *testing.T, f *fakeTarget, entries []schedule.Entry, change func(*Config)) recording {
	t.Helper()
	server := httptest.NewServer(f)
	defer server.Close()
	client := contract.New(contract.NewHTTPClient(10*time.Second), server.URL, "http://origin.test", "x-instance")
	session, err := client.SignIn(context.Background(), "storm-0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Schedule:  schedule.Schedule{Version: schedule.Version, Params: schedule.Params{Bidders: 1}, Entries: entries},
		AuctionID: "aaaaaaaa-0000", Sessions: []contract.Session{session},
		MaxInFlight: 1000, PriceEvery: 10 * time.Millisecond,
		PollEvery: 5 * time.Millisecond, PollTimeout: time.Second,
	}
	if change != nil {
		change(&cfg)
	}
	path := filepath.Join(t.TempDir(), "events.jsonl")
	w, err := record.NewWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	out := recording{bids: map[int]record.Bid{}}
	out.res, out.instances, out.err = Run(context.Background(), cfg, client, w, record.NewClock())
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out.raw = string(raw)
	lines := bufio.NewScanner(strings.NewReader(out.raw))
	for lines.Scan() {
		var head struct{ Type string }
		if err := json.Unmarshal(lines.Bytes(), &head); err != nil {
			t.Fatal(err)
		}
		switch head.Type {
		case record.TypeBid:
			var b record.Bid
			json.Unmarshal(lines.Bytes(), &b)
			out.bids[b.Seq] = b
		case record.TypePoll:
			var p record.Poll
			json.Unmarshal(lines.Bytes(), &p)
			out.polls = append(out.polls, p)
		}
	}
	if len(f.bad) > 0 {
		t.Fatalf("client mistakes: %v", f.bad)
	}
	return out
}

func every(n int, gap time.Duration) []schedule.Entry {
	entries := make([]schedule.Entry, n)
	for i := range entries {
		entries[i] = schedule.Entry{Seq: i, OffsetUS: int64(i) * gap.Microseconds()}
	}
	return entries
}

// The open-model property (ADR 0004): a target that answers nothing doesn't slow the sends.
func TestSendsDoNotWaitForReplies(t *testing.T) {
	const n = 20
	var all sync.WaitGroup
	all.Add(n)
	release := make(chan struct{})
	go func() {
		all.Wait() // every bid has arrived, with none answered yet
		close(release)
	}()
	f := &fakeTarget{bid: func(w http.ResponseWriter, key string, amount int64) {
		all.Done()
		select {
		case <-release:
			accept(w, key, amount)
		case <-time.After(5 * time.Second): // a generator that waits would end up here
			w.WriteHeader(http.StatusInternalServerError)
		}
	}}
	got := storm(t, f, every(n, 5*time.Millisecond), nil)
	if got.err != nil || got.res.HTTPStatuses["201"] != n || got.res.AcceptedBids != n {
		t.Fatalf("%v %+v", got.err, got.res)
	}
	for _, b := range got.bids {
		if b.IntendedUS > b.SentUS || b.SentUS > b.RepliedUS {
			t.Fatalf("times out of order: %+v", b)
		}
	}
	if span := f.arrived[n-1].Sub(f.arrived[0]); span < 50*time.Millisecond {
		t.Fatalf("sends bunched up in %s instead of following the schedule", span)
	}
	if strings.Join(got.instances, ",") != "3001" {
		t.Fatalf("instances %v", got.instances)
	}
}

func TestPendingBidsArePolled(t *testing.T) {
	f := &fakeTarget{bid: func(w http.ResponseWriter, _ string, _ int64) {
		w.WriteHeader(http.StatusAccepted)
		io.WriteString(w, `{"bid":{"id":"b1","status":"pending","reason":null}}`)
	}}
	got := storm(t, f, every(1, 0), nil)
	if len(got.polls) != 1 || got.polls[0].N != 1 || *got.polls[0].BidStatus != "accepted" {
		t.Fatalf("polls %+v", got.polls)
	}
	if got.res.AcceptedBids != 1 || got.res.UnresolvedBids != 0 || got.res.LastStatuses["accepted"] != 1 {
		t.Fatalf("%+v", got.res)
	}
}

func TestRetriesReuseTheKey(t *testing.T) {
	first := 0
	entries := every(3, 5*time.Millisecond)
	entries[0].Steps = 2
	entries[1].RetryOf, entries[1].Steps = &first, 2
	entries[2].RetryOf, entries[2].Steps, entries[2].ChangeAmount = &first, 2, true
	got := storm(t, &fakeTarget{bid: accept}, entries, nil)
	a, same, changed := got.bids[0], got.bids[1], got.bids[2]
	if a.Amount != 1200 || same.Amount != 1200 || changed.Amount != 1300 {
		t.Fatalf("amounts %d %d %d", a.Amount, same.Amount, changed.Amount)
	}
	if a.Key != "aaaaaaaa-0" || same.Key != a.Key || changed.Key != a.Key {
		t.Fatalf("keys %q %q %q", a.Key, same.Key, changed.Key)
	}
}

func TestCapSkipsAndInvalidates(t *testing.T) {
	f := &fakeTarget{bid: func(w http.ResponseWriter, key string, amount int64) {
		time.Sleep(200 * time.Millisecond)
		accept(w, key, amount)
	}}
	got := storm(t, f, every(5, time.Millisecond), func(c *Config) { c.MaxInFlight = 1 })
	if got.res.Sent != 1 || got.res.Skipped != 4 || strings.Count(got.raw, `"type":"skipped"`) != 4 {
		t.Fatalf("%+v", got.res)
	}
	if reasons := InvalidReasons(got.res, time.Second, nil, nil); len(reasons) != 1 {
		t.Fatalf("reasons %v", reasons)
	}
}

func TestFailuresAreRecordedWithoutSecrets(t *testing.T) {
	f := &fakeTarget{bid: func(w http.ResponseWriter, key string, _ int64) {
		if strings.HasSuffix(key, "-0") {
			panic(http.ErrAbortHandler) // drop the connection
		}
		w.Header().Set("Set-Cookie", "holdfast_session="+token)
		w.WriteHeader(http.StatusUnprocessableEntity)
		io.WriteString(w, `{"error":"idempotency_conflict","message":"`+token+`"}`)
	}}
	got := storm(t, f, every(2, 5*time.Millisecond), nil)
	if got.bids[0].TransportError == nil || got.bids[0].HTTPStatus != nil {
		t.Fatalf("dropped bid %+v", got.bids[0])
	}
	if *got.bids[1].ErrorCode != "idempotency_conflict" {
		t.Fatalf("error bid %+v", got.bids[1])
	}
	// A dropped request must not be quietly sent again.
	if len(f.arrived) != 2 {
		t.Fatalf("%d requests for 2 sends", len(f.arrived))
	}
	if strings.Contains(got.raw, "SESSIONTOKEN") {
		t.Fatal("the session token is in the recording")
	}
}

func TestVerdict(t *testing.T) {
	if r := InvalidReasons(record.Results{SendLagMaxUS: 1000}, 50*time.Millisecond, nil, nil); len(r) != 0 {
		t.Fatal(r)
	}
	r := InvalidReasons(record.Results{SendLagMaxUS: 60_000}, 50*time.Millisecond, context.Canceled, errors.New("disk"))
	if len(r) != 3 {
		t.Fatal(r)
	}
}
