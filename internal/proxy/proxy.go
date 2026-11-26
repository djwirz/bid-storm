// Package proxy is the recording proxy of ADR 0003: it sits between the target and its
// Stripe fake, passes everything through unchanged and records authorize, cancel and capture.
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/djwirz/bid-storm/internal/contract"
	"github.com/djwirz/bid-storm/internal/record"
	"github.com/djwirz/bid-storm/internal/target"
)

const maxBody = 1 << 20

// Proxy is an http.Handler. Until Record is called it passes through without recording.
type Proxy struct {
	upstream string
	client   *http.Client

	mu        sync.Mutex // guards everything below
	events    *record.Writer
	clock     record.Clock
	auctionID string
	intents   map[string]*intent // by intent id: cancel and capture name nothing else
	asking    map[string]bool    // bid ids whose authorize has no answer yet
	held      map[string]bool    // bid ids that got a hold
	calls     map[string]int     // by op
}

type intent struct {
	bidID, auctionID string
	settled          bool // released, captured, or refused for good
}

// New makes a proxy for a fake on this machine.
func New(upstream string) (*Proxy, error) {
	if err := target.CheckLoopback(upstream); err != nil {
		return nil, fmt.Errorf("fake: %w", err)
	}
	return &Proxy{
		upstream: strings.TrimRight(upstream, "/"),
		client:   contract.NewHTTPClient(30 * time.Second),
		intents:  map[string]*intent{}, asking: map[string]bool{}, held: map[string]bool{}, calls: map[string]int{},
	}, nil
}

// Record starts recording the holds of one auction.
func (p *Proxy) Record(events *record.Writer, clock record.Clock, auctionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events, p.clock, p.auctionID = events, clock, auctionID
}

// Outstanding counts holds the target still has to finish: authorizations with no answer yet,
// and holds neither released nor captured.
func (p *Proxy) Outstanding() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := len(p.asking)
	for _, in := range p.intents {
		if in.auctionID == p.auctionID && !in.settled {
			n++
		}
	}
	return n
}

// Held reports which bids were seen to get a hold, and how many requests of each kind passed.
func (p *Proxy) Held() (held map[string]bool, calls map[string]int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	held, calls = map[string]bool{}, map[string]int{}
	for id := range p.held {
		held[id] = true
	}
	for op, n := range p.calls {
		calls[op] = n
	}
	return held, calls
}

// ServeHTTP forwards one request to the fake.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	received := time.Now()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	op := ""
	if r.Method == http.MethodPost {
		switch r.URL.Path {
		case "/authorize", "/cancel", "/capture":
			op = r.URL.Path[1:]
		}
	}
	var ex exchange
	if op != "" {
		ex = p.begin(op, body)
	}

	status, header, reply, err := p.forward(r, body)
	replied := time.Now()
	// Noted before the target gets the answer, so a cancel that follows finds its intent.
	if op != "" {
		p.finish(ex, received, replied, status, reply, err)
	}
	if err != nil {
		// A 5xx, which the target treats as "outcome unknown", as it would a dead fake.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, `{"error":"bad_gateway"}`)
		return
	}
	copyHeader(w.Header(), header)
	w.WriteHeader(status)
	w.Write(reply)
}

func (p *Proxy) forward(r *http.Request, body []byte) (int, http.Header, []byte, error) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, p.upstream+r.URL.RequestURI(), bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	copyHeader(req.Header, r.Header)
	req.GetBody = nil // never re-send: the target's requests reach the fake once
	res, err := p.client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer res.Body.Close()
	reply, err := io.ReadAll(io.LimitReader(res.Body, 64*maxBody)) // /audit lists every intent
	return res.StatusCode, res.Header, reply, err
}

// copyHeader copies end-to-end headers; the rest describe one connection.
func copyHeader(dst, src http.Header) {
	for name, values := range src {
		switch name {
		case "Connection", "Keep-Alive", "Transfer-Encoding", "Content-Length", "Te", "Trailer", "Upgrade", "Proxy-Connection":
			continue
		}
		dst[name] = values
	}
}

// exchange is what the request said. Fields not listed here (such as userId) are never read.
type exchange struct {
	op              string
	PaymentIntentID string `json:"paymentIntentId"`
	Key             string `json:"idempotencyKey"`
	Amount          *int64 `json:"amount"`
	Metadata        struct {
		BidID     string `json:"bidId"`
		AuctionID string `json:"auctionId"`
	} `json:"metadata"`
}

func (p *Proxy) begin(op string, body []byte) exchange {
	var ex exchange
	json.Unmarshal(body, &ex) // an unreadable body is recorded with what little it gave
	ex.op = op
	p.mu.Lock()
	defer p.mu.Unlock()
	if op == "authorize" && ex.Metadata.BidID != "" && ex.Metadata.AuctionID == p.auctionID {
		p.asking[ex.Metadata.BidID] = true
	}
	return ex
}

func (p *Proxy) finish(ex exchange, received, replied time.Time, status int, reply []byte, forwardErr error) {
	var answer struct {
		OK        *bool  `json:"ok"`
		Intent    string `json:"paymentIntentId"`
		Reason    string `json:"reason"`
		Retryable *bool  `json:"retryable"`
		Error     string `json:"error"`
	}
	if forwardErr == nil {
		json.Unmarshal(reply, &answer)
	}
	bidID, auctionID, intentID := ex.Metadata.BidID, ex.Metadata.AuctionID, ex.PaymentIntentID
	if ex.op == "authorize" {
		intentID = answer.Intent
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if in := p.intents[intentID]; in != nil && ex.op != "authorize" {
		bidID, auctionID = in.bidID, in.auctionID
	}
	// Another run's leftovers pass through but aren't part of this recording.
	if p.events == nil || auctionID != "" && auctionID != p.auctionID {
		return
	}
	p.calls[ex.op]++
	answered := forwardErr == nil && status == http.StatusOK && answer.OK != nil
	switch {
	case !answered: // unknown outcome: the target has to ask again
	case ex.op == "authorize":
		delete(p.asking, bidID)
		if *answer.OK && intentID != "" {
			p.held[bidID] = true
			if p.intents[intentID] == nil { // a replay must not reopen a released hold
				p.intents[intentID] = &intent{bidID: bidID, auctionID: auctionID}
			}
		}
	case p.intents[intentID] != nil:
		if *answer.OK || answer.Retryable != nil && !*answer.Retryable {
			p.intents[intentID].settled = true
		}
	}

	opt := func(s string) *string {
		if s == "" {
			return nil
		}
		s = contract.Clip(s)
		return &s
	}
	e := record.Hold{
		V: record.SchemaVersion, Type: record.TypeHold, Op: ex.op,
		ReceivedUS: p.clock.US(received), RepliedUS: p.clock.US(replied),
		BidID: opt(bidID), AuctionID: opt(auctionID), IntentID: opt(intentID),
		Amount: ex.Amount, Key: opt(ex.Key),
		OK: answer.OK, Reason: opt(answer.Reason), Retryable: answer.Retryable, ErrorCode: opt(answer.Error),
	}
	if forwardErr != nil {
		class := contract.Classify(forwardErr)
		e.TransportError = &class
	} else {
		e.HTTPStatus = &status
	}
	p.events.Write(e)
}
