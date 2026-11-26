// Package contract is the HTTP client for what every target shares: dev sign-in, the auction
// snapshot, placing a bid and following it. What differs per target lives in internal/target.
package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	sessionCookie = "holdfast_session" // named by the shared contract
	maxBody       = 1 << 20
	maxField      = 64 // longest server-supplied string worth recording
)

// Client talks to one target. Safe for many goroutines at once.
type Client struct {
	base, origin, instanceHeader string
	http                         *http.Client
	streams                      *http.Client // the same, without a limit on the whole exchange
}

// NewHTTPClient is the client every request to a target goes through.
func NewHTTPClient(timeout time.Duration) *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil // the target is on this machine; never send it through a proxy
	// The default keeps 2 idle connections per host, which would open a new one for most
	// bids in a storm and measure TCP setup instead of the target.
	t.MaxIdleConns = 0 // no overall limit
	t.MaxIdleConnsPerHost = 2048
	// Node closes a connection idle for 5s. Drop ours first, so a bid isn't written into
	// one the server is closing (it wouldn't be re-sent: see do).
	t.IdleConnTimeout = 4 * time.Second
	return &http.Client{
		Transport: t,
		Timeout:   timeout,
		// A redirect could leave the machine; report it as the reply instead.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// New makes a client. origin is sent on every POST; instanceHeader names the response header
// that says which instance answered, or is empty.
func New(hc *http.Client, base, origin, instanceHeader string) *Client {
	streams := *hc
	streams.Timeout = 0 // a live stream lasts as long as the auction; its context ends it
	return &Client{base: strings.TrimRight(base, "/"), origin: origin, instanceHeader: instanceHeader, http: hc, streams: &streams}
}

// Session is a signed-in bidder's cookie. It prints as a placeholder so it can't be logged.
type Session struct{ token string }

// String hides the token from fmt and logs.
func (Session) String() string { return "[session]" }

// SignIn uses the development sign-in route.
func (c *Client) SignIn(ctx context.Context, login string) (Session, error) {
	res, err := c.do(ctx, http.MethodPost, "/api/auth/dev", Session{}, "", map[string]string{"login": login})
	if err != nil {
		return Session{}, fmt.Errorf("sign in: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		return Session{}, fmt.Errorf("sign in: HTTP %d", res.StatusCode)
	}
	for _, ck := range res.Cookies() {
		if ck.Name == sessionCookie && ck.Value != "" {
			return Session{token: ck.Value}, nil
		}
	}
	return Session{}, errors.New("sign in: no session cookie")
}

// Snapshot is the part of an auction the generator needs.
type Snapshot struct {
	Status         string `json:"status"`
	MinIncrement   int64  `json:"minIncrement"`
	MinimumNextBid *int64 `json:"minimumNextBid"` // null once closed
}

// Snapshot reads an auction.
func (c *Client) Snapshot(ctx context.Context, auctionID string) (Snapshot, error) {
	res, err := c.do(ctx, http.MethodGet, "/api/auctions/"+url.PathEscape(auctionID), Session{}, "", nil)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read auction: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Snapshot{}, fmt.Errorf("read auction: HTTP %d", res.StatusCode)
	}
	var body struct {
		Auction Snapshot `json:"auction"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxBody)).Decode(&body); err != nil {
		return Snapshot{}, fmt.Errorf("read auction: %w", err)
	}
	return body.Auction, nil
}

// Stream opens an auction's live stream, resuming after lastEventID if it isn't nil. The
// caller closes the body. instance is whichever instance answered, if the target says.
func (c *Client) Stream(ctx context.Context, auctionID string, lastEventID *int64) (res *http.Response, instance string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/auctions/"+url.PathEscape(auctionID)+"/events", nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID != nil {
		req.Header.Set("Last-Event-ID", strconv.FormatInt(*lastEventID, 10))
	}
	res, err = c.streams.Do(req)
	if err != nil {
		return nil, "", err
	}
	if c.instanceHeader != "" {
		instance = Clip(res.Header.Get(c.instanceHeader))
	}
	return res, instance, nil
}

// Result is the outcome of one bid or poll request. Empty fields weren't in the reply.
type Result struct {
	HTTPStatus     int // 0 if no response arrived
	Instance       string
	BidID          string
	BidStatus      string
	Reason         string
	ErrorCode      string
	TransportError string // a fixed class, never the error's text
}

// PlaceBid posts a bid. It always returns a Result: a failure is something to record.
func (c *Client) PlaceBid(ctx context.Context, s Session, auctionID, key string, amount int64) Result {
	path := "/api/auctions/" + url.PathEscape(auctionID) + "/bids"
	return c.result(c.do(ctx, http.MethodPost, path, s, key, map[string]int64{"amount": amount}))
}

// Bid reads one of the bidder's own bids.
func (c *Client) Bid(ctx context.Context, s Session, bidID string) Result {
	return c.result(c.do(ctx, http.MethodGet, "/api/bids/"+url.PathEscape(bidID), s, "", nil))
}

func (c *Client) do(ctx context.Context, method, path string, s Session, key string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", c.origin) // the target refuses cross-origin writes
	}
	if s.token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: s.token})
	}
	if key != "" {
		// net/http quietly re-sends a request that has an Idempotency-Key when a reused
		// connection drops, which would be a send the recording never shows. It can only
		// do that if it can rewind the body, so take that ability away.
		req.Header.Set("Idempotency-Key", key)
		req.GetBody = nil
	}
	return c.http.Do(req)
}

func (c *Client) result(res *http.Response, err error) Result {
	if err != nil {
		return Result{TransportError: Classify(err)}
	}
	defer res.Body.Close()
	out := Result{HTTPStatus: res.StatusCode}
	if c.instanceHeader != "" {
		out.Instance = Clip(res.Header.Get(c.instanceHeader))
	}
	// Read to the end so the connection can be reused.
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		out.TransportError = Classify(err)
		return out
	}
	var body struct {
		Bid *struct {
			ID     string  `json:"id"`
			Status string  `json:"status"`
			Reason *string `json:"reason"`
		} `json:"bid"`
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &body) != nil {
		out.TransportError = "bad_response"
		return out
	}
	out.ErrorCode = Clip(body.Error)
	if b := body.Bid; b != nil {
		out.BidID, out.BidStatus = Clip(b.ID), Clip(b.Status)
		if b.Reason != nil {
			out.Reason = Clip(*b.Reason)
		}
	}
	return out
}

// Clip bounds a server-supplied string before it is recorded.
func Clip(s string) string {
	if len(s) > maxField {
		return s[:maxField]
	}
	return s
}

// Classify names a transport failure with a fixed word, so no error text is recorded.
func Classify(err error) string {
	var timeout net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.As(err, &timeout) && timeout.Timeout():
		return "timeout"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "connection_closed"
	default:
		return "other"
	}
}
