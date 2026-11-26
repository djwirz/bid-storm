package stream

import (
	"context"
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
)

// The first connection sends a snapshot and one bid, then drops. The resume must ask for
// what came after that bid, and gets the rest.
func TestResumesAndStopsAtClose(t *testing.T) {
	var mu sync.Mutex
	var resumedFrom []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last := r.Header.Get("Last-Event-ID")
		mu.Lock()
		resumedFrom = append(resumedFrom, last)
		mu.Unlock()
		w.Header().Set("x-instance", "3001")
		io.WriteString(w, "retry: 2000\n\n")
		if last == "" {
			io.WriteString(w, "event: snapshot\nid: 0\ndata: {\"auction\":{\"version\":0,\"status\":\"open\"}}\n\n")
			io.WriteString(w, ": keep-alive\n\n")
			io.WriteString(w, "event: bid.accepted\nid: 1\ndata: {\"type\":\"bid.accepted\",\"version\":1,\"amount\":1000,\"bidder\":\"Bidder ab12\",\"at\":\"2026-01-01T00:00:00Z\"}\n\n")
			return
		}
		fmt.Fprint(w, "event: bid.accepted\nid: 3\ndata: {\"version\":3,\"amount\":1200}\n\n")
		fmt.Fprint(w, "event: auction.closed\nid: 4\ndata: {\"version\":4,\"status\":\"closed\",\"winner\":\"Bidder cd34\",\"amount\":1200}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done() // a real stream stays open after the close
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	w, err := record.NewWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	client := contract.New(contract.NewHTTPClient(time.Second), server.URL, "", "x-instance")
	g, err := Subscribe(context.Background(), client, "a1", 1, w, record.NewClock())
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); g.Waiting() != ""; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal(g.Waiting())
		}
	}
	got := g.Stop()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got.Events != 4 || got.Reconnects != 1 || !got.Announced[0][1000] || !got.Announced[0][1200] || strings.Join(got.Instances, ",") != "3001" {
		t.Fatalf("%+v", got)
	}
	if strings.Join(resumedFrom, ",") != ",1" {
		t.Fatalf("Last-Event-ID %q", resumedFrom)
	}
	raw, _ := os.ReadFile(path)
	for _, want := range []string{`"event":"snapshot","version":0`, `"server_at":"2026-01-01T00:00:00Z"`, `"event":"auction.closed","version":4,"amount":1200,"bidder":"Bidder cd34"`, `"last_event_id":1`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("recording lacks %s", want)
		}
	}
}
