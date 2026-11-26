package record

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentWritesGiveWholeLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	w, err := NewWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	const n = 2000
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { w.Write(Bid{V: SchemaVersion, Type: TypeBid, Seq: i, Key: strings.Repeat("k", 100)}) })
	}
	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	seen := map[int]bool{}
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		var b Bid
		if err := json.Unmarshal(lines.Bytes(), &b); err != nil {
			t.Fatalf("broken line: %v", err)
		}
		seen[b.Seq] = true
	}
	if len(seen) != n {
		t.Fatalf("%d distinct events, want %d", len(seen), n)
	}
}

// The exact bytes are the format other tools read (ADR 0007). A change here that renames,
// removes or redefines a field needs a new SchemaVersion.
func TestEventFormat(t *testing.T) {
	status, id, state := 201, "b1", "accepted"
	reply := Reply{SentUS: 11, RepliedUS: 12, HTTPStatus: &status, BidID: &id, BidStatus: &state}
	path := filepath.Join(t.TempDir(), "events.jsonl")
	w, err := NewWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(Bid{V: 1, Type: TypeBid, Seq: 3, Bidder: 2, IntendedUS: 10, Key: "a-3", Amount: 1100, MinNextSeen: 1100, PriceAgeUS: 5, Reply: reply})
	w.Write(Poll{V: 1, Type: TypePoll, Seq: 3, N: 1, Reply: reply})
	w.Write(Skipped{V: 1, Type: TypeSkipped, Seq: 4, Bidder: 1, IntendedUS: 20, AtUS: 21, Why: "in_flight_cap"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const tail = `"sent_us":11,"replied_us":12,"http_status":201,"instance":null,"bid_id":"b1","bid_status":"accepted","reason":null,"error_code":null,"transport_error":null}` + "\n"
	want := `{"v":1,"type":"bid","seq":3,"bidder":2,"intended_us":10,"idempotency_key":"a-3","amount":1100,"min_next_seen":1100,"price_age_us":5,"retry_of":null,` + tail +
		`{"v":1,"type":"poll","seq":3,"n":1,` + tail +
		`{"v":1,"type":"skipped","seq":4,"bidder":1,"intended_us":20,"at_us":21,"why":"in_flight_cap"}` + "\n"
	if string(got) != want {
		t.Fatalf("format changed:\n got %s\nwant %s", got, want)
	}
}

func TestWriterRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWriter(path); err == nil {
		t.Fatal("overwrote a recording")
	}
}

func TestClock(t *testing.T) {
	c := NewClock()
	a := time.Now()
	b := a.Add(1500 * time.Microsecond)
	if d := c.US(b) - c.US(a); d < 1499 || d > 1501 {
		t.Fatalf("difference %d, want 1500", d)
	}
}
