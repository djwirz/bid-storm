// Package stream records an auction's live stream (server-sent events) from several
// subscribers at once, to show whether they were told the same things, and when.
package stream

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/djwirz/bid-storm/internal/contract"
	"github.com/djwirz/bid-storm/internal/record"
)

// reconnectAfter is how long a dropped subscriber waits before resuming.
const reconnectAfter = 500 * time.Millisecond

// Group is a set of subscribers to one auction.
type Group struct {
	client    *contract.Client
	auctionID string
	events    *record.Writer
	clock     record.Clock
	stop      context.CancelFunc
	running   sync.WaitGroup

	mu        sync.Mutex // guards everything below
	announced []map[int64]bool
	closed    []bool
	count     int
	reconns   int
	instances map[string]bool
}

// Summary is what the subscribers saw.
type Summary struct {
	Announced  []map[int64]bool // per subscriber: the amounts announced as accepted
	Events     int
	Reconnects int
	Instances  []string
}

// Subscribe connects n subscribers, one after another so a round-robin target spreads them,
// and returns once each has its snapshot: nothing sent after that can be missed.
func Subscribe(ctx context.Context, client *contract.Client, auctionID string, n int, events *record.Writer, clock record.Clock) (*Group, error) {
	ctx, stop := context.WithCancel(ctx)
	g := &Group{
		client: client, auctionID: auctionID, events: events, clock: clock, stop: stop,
		announced: make([]map[int64]bool, n), closed: make([]bool, n), instances: map[string]bool{},
	}
	for i := range n {
		g.announced[i] = map[int64]bool{}
		ready := make(chan struct{})
		g.running.Go(func() { g.follow(ctx, i, ready) })
		select {
		case <-ready:
		case <-time.After(10 * time.Second):
			g.Stop()
			return nil, fmt.Errorf("subscriber %d got no snapshot", i)
		case <-ctx.Done():
			g.Stop()
			return nil, ctx.Err()
		}
	}
	return g, nil
}

// Waiting says what the subscribers haven't seen yet, or "" once each has seen the close.
func (g *Group) Waiting() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	missing := 0
	for _, c := range g.closed {
		if !c {
			missing++
		}
	}
	if missing == 0 {
		return ""
	}
	return fmt.Sprintf("%d of %d subscribers have not seen the auction close", missing, len(g.closed))
}

// Stop disconnects everyone and reports what was seen.
func (g *Group) Stop() Summary {
	g.stop()
	g.running.Wait()
	g.mu.Lock()
	defer g.mu.Unlock()
	s := Summary{Announced: g.announced, Events: g.count, Reconnects: g.reconns}
	for i := range g.instances {
		s.Instances = append(s.Instances, i)
	}
	slices.Sort(s.Instances)
	return s
}

// follow keeps one subscriber connected until it sees the close or is stopped.
func (g *Group) follow(ctx context.Context, sub int, ready chan struct{}) {
	var lastID *int64
	markReady := sync.OnceFunc(func() { close(ready) })
	for n := 1; ctx.Err() == nil; n++ {
		if g.connect(ctx, sub, n, &lastID, markReady) {
			return
		}
		select {
		case <-time.After(reconnectAfter):
		case <-ctx.Done():
		}
	}
}

// connect reads one connection to its end. It reports whether the auction closed.
func (g *Group) connect(ctx context.Context, sub, n int, lastID **int64, ready func()) bool {
	conn := record.Conn{V: record.SchemaVersion, Type: record.TypeConn, Subscriber: sub, N: n, LastEventID: *lastID}
	mark := func(event string, err error) {
		conn.Event, conn.AtUS, conn.TransportError = event, g.clock.US(time.Now()), nil
		if err != nil && ctx.Err() == nil {
			class := contract.Classify(err)
			conn.TransportError = &class
		}
		g.events.Write(conn)
	}
	res, instance, err := g.client.Stream(ctx, g.auctionID, *lastID)
	if err != nil {
		mark("end", err)
		return false
	}
	defer res.Body.Close()
	conn.HTTPStatus = &res.StatusCode
	if instance != "" {
		conn.Instance = &instance
	}
	if res.StatusCode != http.StatusOK {
		mark("end", nil)
		return false
	}
	mark("open", nil)
	g.mu.Lock()
	if n > 1 {
		g.reconns++
	}
	if instance != "" {
		g.instances[instance] = true
	}
	g.mu.Unlock()

	closed := false
	err = readFrames(res.Body, func(f frame) bool {
		arrived := time.Now()
		e, ok := g.parse(sub, arrived, f)
		if !ok {
			return true
		}
		g.events.Write(e)
		version := e.Version
		*lastID = &version
		g.mu.Lock()
		defer g.mu.Unlock()
		g.count++
		switch e.Event {
		case "snapshot":
			ready()
		case "bid.accepted":
			if e.Amount != nil {
				g.announced[sub][*e.Amount] = true
			}
		case "auction.closed":
			g.closed[sub], closed = true, true
		}
		return !closed
	})
	if closed {
		err = nil
	} else if err == nil {
		err = io.EOF // the server ended a stream that should stay open
	}
	mark("end", err)
	return closed
}

// parse keeps only the fields the recording names.
func (g *Group) parse(sub int, arrived time.Time, f frame) (record.Stream, bool) {
	var d struct {
		Version int64   `json:"version"`
		Amount  *int64  `json:"amount"`
		Bidder  *string `json:"bidder"`
		Winner  *string `json:"winner"`
		At      *string `json:"at"`
		Status  *string `json:"status"`
		Auction *struct {
			Version int64  `json:"version"`
			Status  string `json:"status"`
		} `json:"auction"`
	}
	switch f.event {
	case "snapshot", "bid.accepted", "auction.closed":
	default:
		return record.Stream{}, false
	}
	if json.Unmarshal([]byte(f.data), &d) != nil {
		return record.Stream{}, false
	}
	if d.Auction != nil {
		d.Version, d.Status = d.Auction.Version, &d.Auction.Status
	}
	if d.Winner != nil {
		d.Bidder = d.Winner
	}
	clip := func(s *string) *string {
		if s == nil {
			return nil
		}
		c := contract.Clip(*s)
		return &c
	}
	return record.Stream{
		V: record.SchemaVersion, Type: record.TypeStream, Subscriber: sub,
		ArrivedUS: g.clock.US(arrived), Event: f.event, Version: d.Version,
		Amount: d.Amount, Bidder: clip(d.Bidder), ServerAt: clip(d.At), Status: clip(d.Status),
	}, true
}

type frame struct{ event, data string }

// readFrames parses server-sent events: "field: value" lines, a blank line ending each
// event, and ":" comments as keep-alives. each returns false to stop reading.
func readFrames(r io.Reader, each func(frame) bool) error {
	lines := bufio.NewScanner(r)
	lines.Buffer(make([]byte, 64<<10), 1<<20)
	var f frame
	for lines.Scan() {
		line := lines.Text()
		if line == "" {
			if f.data != "" && !each(f) {
				return nil
			}
			f = frame{}
			continue
		}
		name, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch name {
		case "event":
			f.event = value
		case "data":
			if f.data != "" {
				f.data += "\n"
			}
			f.data += value
		}
	}
	if err := lines.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
