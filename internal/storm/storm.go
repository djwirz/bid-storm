// Package storm sends a schedule at a target and records what happens.
package storm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/djwirz/bid-storm/internal/contract"
	"github.com/djwirz/bid-storm/internal/record"
	"github.com/djwirz/bid-storm/internal/schedule"
)

// Config is one storm against an auction that already exists.
type Config struct {
	Schedule    schedule.Schedule
	AuctionID   string
	Sessions    []contract.Session // one per bidder
	MaxInFlight int                // bids awaiting a reply; past this, sends are skipped, never delayed
	PriceEvery  time.Duration
	PollEvery   time.Duration
	PollTimeout time.Duration
}

type price struct {
	minimumNext int64
	at          time.Time
}

type run struct {
	cfg    Config
	client *contract.Client
	events *record.Writer
	clock  record.Clock

	price    atomic.Pointer[price] // written by the price reader, read by the dispatcher
	inFlight atomic.Int64

	mu       sync.Mutex // guards the tally below, shared by every send
	res      record.Results
	lags     []int64
	final    map[string]string // bid id → last status seen
	accepted map[string]bool
	pending  map[string]bool
	seen     map[string]bool // instances
}

// Run sends the schedule on time whether or not replies have come back (ADR 0004), then
// waits for outstanding replies and pending bids. It returns the instances seen in replies.
// If ctx is cancelled it stops early and returns ctx's error with the results so far.
func Run(ctx context.Context, cfg Config, client *contract.Client, events *record.Writer, clock record.Clock) (record.Results, []string, error) {
	if len(cfg.Sessions) < cfg.Schedule.Params.Bidders {
		return record.Results{}, nil, errors.New("fewer sessions than bidders")
	}
	if cfg.MaxInFlight <= 0 || cfg.PriceEvery <= 0 || cfg.PollEvery <= 0 {
		return record.Results{}, nil, errors.New("in-flight cap and intervals must be positive")
	}
	r := &run{
		cfg: cfg, client: client, events: events, clock: clock,
		res: record.Results{
			Scheduled:    len(cfg.Schedule.Entries),
			HTTPStatuses: map[string]int{}, TransportErrors: map[string]int{}, LastStatuses: map[string]int{},
		},
		final: map[string]string{}, accepted: map[string]bool{}, pending: map[string]bool{}, seen: map[string]bool{},
	}

	snap, err := client.Snapshot(ctx, cfg.AuctionID)
	if err != nil {
		return r.res, nil, err
	}
	if snap.MinimumNextBid == nil || snap.MinIncrement <= 0 {
		return r.res, nil, fmt.Errorf("auction is not taking bids (status %q)", snap.Status)
	}
	r.price.Store(&price{minimumNext: *snap.MinimumNextBid, at: time.Now()})

	priceCtx, stopPrice := context.WithCancel(ctx)
	var reader sync.WaitGroup
	reader.Go(func() { r.readPrice(priceCtx) })

	var sends sync.WaitGroup
	r.dispatch(ctx, snap.MinIncrement, &sends)
	sends.Wait()
	stopPrice()
	reader.Wait()

	return r.results(), r.instances(), ctx.Err()
}

// dispatch is the only goroutine that decides what to send, so amounts and retries need no lock.
func (r *run) dispatch(ctx context.Context, increment int64, sends *sync.WaitGroup) {
	amounts := make([]int64, len(r.cfg.Schedule.Entries)) // what each entry offered
	prices := make([]price, len(r.cfg.Schedule.Entries))  // and the price it was based on
	// Reset below replaces this first, unused tick.
	timer := time.NewTimer(0)
	defer timer.Stop()
	start := time.Now()
	for _, e := range r.cfg.Schedule.Entries {
		due := start.Add(time.Duration(e.OffsetUS) * time.Microsecond)
		timer.Reset(time.Until(due)) // already due fires at once: late sends catch up
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}

		p := *r.price.Load()
		amount := p.minimumNext + int64(e.Steps)*increment
		keyOf := e.Seq
		if e.RetryOf != nil {
			keyOf = *e.RetryOf
			p, amount = prices[keyOf], amounts[keyOf]
			if e.ChangeAmount {
				amount += increment
			}
		}
		amounts[e.Seq], prices[e.Seq] = amount, p

		bid := record.Bid{
			V: record.SchemaVersion, Type: record.TypeBid,
			Seq: e.Seq, Bidder: e.Bidder, IntendedUS: r.clock.US(due),
			// Keys are per bidder across auctions, so they carry the auction.
			Key:    fmt.Sprintf("%.8s-%d", r.cfg.AuctionID, keyOf),
			Amount: amount, MinNextSeen: p.minimumNext,
			PriceAgeUS: max(0, due.Sub(p.at).Microseconds()),
			RetryOf:    e.RetryOf,
		}
		if r.inFlight.Load() >= int64(r.cfg.MaxInFlight) {
			r.events.Write(record.Skipped{
				V: record.SchemaVersion, Type: record.TypeSkipped,
				Seq: e.Seq, Bidder: e.Bidder, IntendedUS: bid.IntendedUS,
				AtUS: r.clock.US(time.Now()), Why: "in_flight_cap",
			})
			r.mu.Lock()
			r.res.Skipped++
			r.mu.Unlock()
			continue
		}
		r.inFlight.Add(1)
		sends.Go(func() { r.send(ctx, bid) })
	}
}

func (r *run) send(ctx context.Context, bid record.Bid) {
	session := r.cfg.Sessions[bid.Bidder]
	sent := time.Now()
	out := r.client.PlaceBid(ctx, session, r.cfg.AuctionID, bid.Key, bid.Amount)
	replied := time.Now()
	r.inFlight.Add(-1)

	bid.Reply = r.reply(sent, replied, out)
	r.events.Write(bid)

	r.mu.Lock()
	r.res.Sent++
	r.lags = append(r.lags, bid.SentUS-bid.IntendedUS)
	if out.HTTPStatus != 0 {
		r.res.HTTPStatuses[strconv.Itoa(out.HTTPStatus)]++
	}
	r.note(out)
	r.mu.Unlock()

	if out.BidStatus == "pending" && out.BidID != "" {
		r.follow(ctx, session, bid.Seq, out.BidID)
	}
}

// follow polls a pending bid until it resolves or the timeout passes.
func (r *run) follow(ctx context.Context, session contract.Session, seq int, bidID string) {
	deadline := time.Now().Add(r.cfg.PollTimeout)
	for n := 1; time.Now().Before(deadline); n++ {
		select {
		case <-time.After(r.cfg.PollEvery):
		case <-ctx.Done():
			return
		}
		sent := time.Now()
		out := r.client.Bid(ctx, session, bidID)
		replied := time.Now()
		// An error reply carries no bid, so say which one was asked about.
		out.BidID = bidID
		r.events.Write(record.Poll{
			V: record.SchemaVersion, Type: record.TypePoll,
			Seq: seq, N: n, Reply: r.reply(sent, replied, out),
		})
		r.mu.Lock()
		r.res.Polls++
		r.note(out)
		r.mu.Unlock()
		if out.BidStatus != "" && out.BidStatus != "pending" {
			return
		}
	}
}

// note updates the tally from one reply. The caller holds r.mu.
func (r *run) note(out contract.Result) {
	if out.TransportError != "" {
		r.res.TransportErrors[out.TransportError]++
	}
	if out.Instance != "" {
		r.seen[out.Instance] = true
	}
	if out.BidID == "" || out.BidStatus == "" {
		return
	}
	switch out.BidStatus {
	case "pending":
		// A late "pending" (a retry racing a poll) must not undo a resolution.
		if _, resolved := r.final[out.BidID]; !resolved {
			r.pending[out.BidID] = true
		}
		return
	case "accepted", "outbid", "won": // each means it was accepted at some point
		r.accepted[out.BidID] = true
	}
	delete(r.pending, out.BidID)
	status := out.BidStatus
	if out.Reason != "" {
		status += ":" + out.Reason
	}
	r.final[out.BidID] = status
}

func (r *run) reply(sent, replied time.Time, out contract.Result) record.Reply {
	opt := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	rep := record.Reply{
		SentUS: r.clock.US(sent), RepliedUS: r.clock.US(replied),
		Instance: opt(out.Instance), BidID: opt(out.BidID), BidStatus: opt(out.BidStatus),
		Reason: opt(out.Reason), ErrorCode: opt(out.ErrorCode), TransportError: opt(out.TransportError),
	}
	if out.HTTPStatus != 0 {
		rep.HTTPStatus = &out.HTTPStatus
	}
	return rep
}

func (r *run) readPrice(ctx context.Context) {
	tick := time.NewTicker(r.cfg.PriceEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		snap, err := r.client.Snapshot(ctx, r.cfg.AuctionID)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			r.mu.Lock()
			r.res.PriceReadErrors++
			r.mu.Unlock()
		case snap.MinimumNextBid != nil: // null once closed: keep the last price, and let it age
			r.price.Store(&price{minimumNext: *snap.MinimumNextBid, at: time.Now()})
		}
	}
}

func (r *run) results() record.Results {
	r.mu.Lock()
	defer r.mu.Unlock()
	res := r.res
	if len(r.lags) > 0 {
		slices.Sort(r.lags)
		res.SendLagMaxUS = r.lags[len(r.lags)-1]
		res.SendLagP99US = r.lags[(len(r.lags)*99+99)/100-1] // nearest rank
	}
	for _, status := range r.final {
		res.LastStatuses[status]++
	}
	res.AcceptedBids = len(r.accepted)
	res.UnresolvedBids = len(r.pending)
	return res
}

func (r *run) instances() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []string{}
	for i := range r.seen {
		out = append(out, i)
	}
	slices.Sort(out)
	return out
}
