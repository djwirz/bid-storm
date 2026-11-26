package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/djwirz/bid-storm/internal/record"
)

func TestAuditKeepsOneAuctionAndNamedFields(t *testing.T) {
	reply := `{"intents":[
		{"id":"pi_2","amount":1200,"amountCapturable":1200,"status":"requires_capture","userId":"PRIVATE","metadata":{"bidId":"b2","auctionId":"a1"}},
		{"id":"pi_9","amount":9,"amountCapturable":0,"status":"canceled","metadata":{"bidId":"b9","auctionId":"other"}},
		{"id":"pi_1","amount":1100,"amountCapturable":0,"status":"canceled","metadata":{"bidId":"b1","auctionId":"a1"}}]}`
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/audit" {
			io.WriteString(w, reply)
		}
	}))
	defer fake.Close()

	got, err := Audit(context.Background(), fake.URL, "a1")
	if err != nil {
		t.Fatal(err)
	}
	want := []record.Intent{
		{IntentID: "pi_1", BidID: "b1", Amount: 1100, Status: "canceled"},
		{IntentID: "pi_2", BidID: "b2", Amount: 1200, AmountCapturable: 1200, Status: "requires_capture"},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %+v", got)
	}

	reply = `{}`
	if _, err := Audit(context.Background(), fake.URL, "a1"); err == nil {
		t.Error("a reply without intents read as an empty audit")
	}
	if _, err := Audit(context.Background(), "https://api.stripe.com", "a1"); err == nil {
		t.Error("asked a fake that isn't on this machine")
	}
}
