package holdfast

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

const summary = `{"auctionId":"a1","quiet":true,"status":"closed","endsAt":"x","bidsAccepted":2,"lostRace":1,
"rejected":0,"won":1,"holds":2,"holdsReleased":1,"holdsCaptured":0,"holdsLive":1,"outboxFailed":0,
"violations":[{"invariant":4,"auctionId":"a1","bidId":"b1","message":"a hold is still open"}]}`

// A shell script stands in for node: it writes the summary to the --out path and exits.
func fakeNode(t *testing.T, body string) *Adapter {
	t.Helper()
	dir := checkout(t)
	node := filepath.Join(dir, "node")
	script := "#!/bin/sh\nwhile [ \"$1\" != \"--out\" ]; do shift; done\nout=$2\n" + body + "\n"
	if err := os.WriteFile(node, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	a, err := New(http.DefaultClient, "http://localhost:3000", dir, node, localDB)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestCheckReportsAViolationAsAVerdict(t *testing.T) {
	a := fakeNode(t, "cat > \"$out\" <<'END'\n"+summary+"\nEND\nexit 1")
	c, err := a.Check(context.Background(), "a1", "http://127.0.0.1:4010")
	if err != nil {
		t.Fatal(err)
	}
	if c.ExitCode != 1 || !c.Quiet || c.BidsAccepted != 2 || len(c.Violations) != 1 || *c.Violations[0].BidID != "b1" {
		t.Errorf("%+v", c)
	}
}

func TestNoVerdictIsAnError(t *testing.T) {
	for name, tc := range map[string]struct{ body, auction string }{
		"crashed":       {"exit 1", "a1"},
		"unknown shape": {`echo '{}' > "$out"`, "a1"},
		"wrong auction": {"cat > \"$out\" <<'END'\n" + summary + "\nEND", "another"},
	} {
		a := fakeNode(t, tc.body)
		if c, err := a.Check(context.Background(), tc.auction, "http://127.0.0.1:4010"); err == nil {
			t.Errorf("%s: got %+v", name, c)
		}
	}
}
