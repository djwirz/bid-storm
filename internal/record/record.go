// Package record writes a run's recording: JSON-lines events (ADR 0002, ADR 0007) and the
// manifest. Field reference: docs/recording-format.md.
//
// Only the fields named in these types are ever written. Headers, cookies and raw bodies
// have nowhere to go, which is how recordings stay free of secrets.
package record

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"
	"time"
)

// SchemaVersion is the "v" on every event and the manifest's schema_version.
const SchemaVersion = 1

// Event types.
const (
	TypeBid     = "bid"
	TypePoll    = "poll"
	TypeSkipped = "skipped"
	TypeHold    = "hold"
	TypeStream  = "stream"
	TypeConn    = "stream_conn"
)

// Clock turns times into Unix microseconds as one wall-clock reading plus the monotonic time
// elapsed since, so an NTP adjustment mid-run can't reorder events.
type Clock struct{ anchor time.Time }

// NewClock anchors a clock at now.
func NewClock() Clock { return Clock{anchor: time.Now()} }

// US is t in Unix microseconds. t must come from time.Now (it carries the monotonic reading).
func (c Clock) US(t time.Time) int64 { return c.anchor.UnixMicro() + t.Sub(c.anchor).Microseconds() }

// Reply is what came back from one HTTP exchange. A nil field is written as null.
type Reply struct {
	SentUS     int64   `json:"sent_us"`
	RepliedUS  int64   `json:"replied_us"` // when the body was read, or the send failed
	HTTPStatus *int    `json:"http_status"`
	Instance   *string `json:"instance"` // which target instance answered, if it says
	BidID      *string `json:"bid_id"`
	BidStatus  *string `json:"bid_status"`
	Reason     *string `json:"reason"`
	ErrorCode  *string `json:"error_code"` // the "error" field of an error reply
	// TransportError is a fixed class (timeout, connection_refused, ...), never an error message.
	TransportError *string `json:"transport_error"`
}

// Bid is one POST of a bid.
type Bid struct {
	V           int    `json:"v"`
	Type        string `json:"type"`
	Seq         int    `json:"seq"`
	Bidder      int    `json:"bidder"`
	IntendedUS  int64  `json:"intended_us"` // latency counts from here (spec §5)
	Key         string `json:"idempotency_key"`
	Amount      int64  `json:"amount"`
	MinNextSeen int64  `json:"min_next_seen"` // the price the amount was based on
	PriceAgeUS  int64  `json:"price_age_us"`  // how old that price was at the intended time
	RetryOf     *int   `json:"retry_of"`
	Reply
}

// Poll is one GET of a bid that was answered as pending.
type Poll struct {
	V    int    `json:"v"`
	Type string `json:"type"`
	Seq  int    `json:"seq"` // of the bid being followed
	N    int    `json:"n"`   // 1 for its first poll
	Reply
}

// Skipped is a scheduled send that was never made.
type Skipped struct {
	V          int    `json:"v"`
	Type       string `json:"type"`
	Seq        int    `json:"seq"`
	Bidder     int    `json:"bidder"`
	IntendedUS int64  `json:"intended_us"`
	AtUS       int64  `json:"at_us"`
	Why        string `json:"why"`
}

// Hold is one authorize, cancel or capture as it passed through the recording proxy.
type Hold struct {
	V          int     `json:"v"`
	Type       string  `json:"type"`
	Op         string  `json:"op"`          // authorize, cancel or capture
	ReceivedUS int64   `json:"received_us"` // the target's request reached the proxy
	RepliedUS  int64   `json:"replied_us"`  // the fake's answer was read, or forwarding failed
	BidID      *string `json:"bid_id"`
	AuctionID  *string `json:"auction_id"`
	IntentID   *string `json:"intent_id"`
	Amount     *int64  `json:"amount"`
	Key        *string `json:"idempotency_key"`
	HTTPStatus *int    `json:"http_status"` // from the fake; null if it never answered
	OK         *bool   `json:"ok"`
	Reason     *string `json:"reason"`
	Retryable  *bool   `json:"retryable"`
	ErrorCode  *string `json:"error_code"`
	// TransportError is a fixed class, as in Reply.
	TransportError *string `json:"transport_error"`
}

// Stream is one event as one live-stream subscriber received it.
type Stream struct {
	V          int     `json:"v"`
	Type       string  `json:"type"`
	Subscriber int     `json:"subscriber"`
	ArrivedUS  int64   `json:"arrived_us"`
	Event      string  `json:"event"` // snapshot, bid.accepted or auction.closed
	Version    int64   `json:"version"`
	Amount     *int64  `json:"amount"`
	Bidder     *string `json:"bidder"`    // the target's public alias; the winner on a close
	ServerAt   *string `json:"server_at"` // the target's own time for an accepted bid
	Status     *string `json:"status"`    // the auction's, on a snapshot or close
}

// Conn is a subscriber's connection opening or ending.
type Conn struct {
	V              int     `json:"v"`
	Type           string  `json:"type"`
	Subscriber     int     `json:"subscriber"`
	N              int     `json:"n"`     // 1 for its first connection
	Event          string  `json:"event"` // open or end
	AtUS           int64   `json:"at_us"`
	LastEventID    *int64  `json:"last_event_id"` // what a resume asked to continue from
	Instance       *string `json:"instance"`
	HTTPStatus     *int    `json:"http_status"`
	TransportError *string `json:"transport_error"`
}

// Writer appends events as JSON lines. Safe for many goroutines at once.
type Writer struct {
	mu   sync.Mutex // guards everything below
	file *os.File
	buf  *bufio.Writer
	err  error // the first failure, reported by Close
}

// NewWriter creates path; it refuses to overwrite an existing recording.
func NewWriter(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, err
	}
	return &Writer{file: f, buf: bufio.NewWriterSize(f, 64<<10)}, nil
}

// Write appends one event. Failures are kept and returned by Close, so callers in the middle
// of a storm don't each have to handle them.
func (w *Writer) Write(event any) {
	line, err := json.Marshal(event) // outside the lock: only the append needs to be serial
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return
	}
	if err == nil {
		_, err = w.buf.Write(append(line, '\n'))
	}
	w.err = err
}

// Close flushes and closes the file, returning the first error of the writer's life.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, err := range []error{w.buf.Flush(), w.file.Close()} {
		if w.err == nil {
			w.err = err
		}
	}
	return w.err
}
