// Package schedule builds the open-model send schedule (ADR 0004): which bidder bids when,
// decided entirely from a seed before the storm starts, so replies can't slow the load down.
package schedule

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
)

// Version of the schedule file format.
const Version = 1

// maxEntries keeps a mistyped rate or duration from exhausting memory.
const maxEntries = 1_000_000

// Params are everything the schedule is generated from.
type Params struct {
	Seed         uint64  `json:"seed"`
	Rate         float64 `json:"rate_per_second"`
	DurationUS   int64   `json:"duration_us"`
	Bidders      int     `json:"bidders"`
	RetryRate    float64 `json:"retry_rate"`
	ConflictRate float64 `json:"conflict_rate"` // share of retries that change the amount
}

// Entry is one intended send. The amount isn't here: it depends on the price at send time.
type Entry struct {
	Seq      int   `json:"seq"`
	OffsetUS int64 `json:"offset_us"` // from the start of the storm
	Bidder   int   `json:"bidder"`
	// Steps is how many minimum increments to bid above the lowest acceptable amount.
	Steps int `json:"steps"`
	// RetryOf is the seq of an earlier bid whose idempotency key this one reuses.
	RetryOf      *int `json:"retry_of"`
	ChangeAmount bool `json:"change_amount"` // a retry that should be refused as a conflict
}

// Schedule is what gets saved with a run.
type Schedule struct {
	Version int     `json:"version"`
	Params  Params  `json:"params"`
	Entries []Entry `json:"entries"`
}

// stepChoices makes most bidders offer the minimum and a few jump ahead, like Holdfast's k6 storm.
var stepChoices = []int{0, 0, 0, 1, 2}

func (p Params) validate() error {
	switch {
	case !(p.Rate > 0):
		return errors.New("rate must be positive")
	case p.DurationUS <= 0:
		return errors.New("duration must be positive")
	case p.Bidders <= 0:
		return errors.New("bidders must be positive")
	case !(p.RetryRate >= 0 && p.RetryRate <= 1), !(p.ConflictRate >= 0 && p.ConflictRate <= 1):
		return errors.New("retry and conflict rates must be between 0 and 1")
	case p.Rate*float64(p.DurationUS)/1e6 > maxEntries:
		return fmt.Errorf("rate times duration is over %d bids", maxEntries)
	}
	return nil
}

// Generate makes Poisson arrivals at a constant rate: the gaps between sends are drawn from
// an exponential distribution, so sends bunch up and spread out as independent arrivals do.
// The same Params always give the same schedule.
func Generate(p Params) (Schedule, error) {
	if err := p.validate(); err != nil {
		return Schedule{}, err
	}
	r := rand.New(rand.NewPCG(p.Seed, 0))
	s := Schedule{Version: Version, Params: p, Entries: []Entry{}}
	originals := make([][]int, p.Bidders) // seqs each bidder can retry
	var at float64                        // microseconds
	for {
		at += r.ExpFloat64() / p.Rate * 1e6
		if at >= float64(p.DurationUS) {
			return s, nil
		}
		e := Entry{Seq: len(s.Entries), OffsetUS: int64(at), Bidder: r.IntN(p.Bidders)}
		mine := originals[e.Bidder]
		if len(mine) > 0 && r.Float64() < p.RetryRate {
			of := mine[r.IntN(len(mine))]
			e.RetryOf = &of
			e.Steps = s.Entries[of].Steps
			e.ChangeAmount = r.Float64() < p.ConflictRate
		} else {
			e.Steps = stepChoices[r.IntN(len(stepChoices))]
			originals[e.Bidder] = append(mine, e.Seq)
		}
		s.Entries = append(s.Entries, e)
	}
}

// Validate checks what the runner relies on, for schedules read from a file.
func (s Schedule) Validate() error {
	if s.Version != Version {
		return fmt.Errorf("schedule version %d, want %d", s.Version, Version)
	}
	if err := s.Params.validate(); err != nil {
		return err
	}
	var last int64
	for i, e := range s.Entries {
		switch {
		case e.Seq != i:
			return fmt.Errorf("entry %d has seq %d", i, e.Seq)
		case e.OffsetUS < last:
			return fmt.Errorf("entry %d is out of order", i)
		case e.Bidder < 0 || e.Bidder >= s.Params.Bidders:
			return fmt.Errorf("entry %d has bidder %d", i, e.Bidder)
		case e.Steps < 0:
			return fmt.Errorf("entry %d has negative steps", i)
		}
		if of := e.RetryOf; of != nil {
			if *of < 0 || *of >= i || s.Entries[*of].Bidder != e.Bidder || s.Entries[*of].RetryOf != nil {
				return fmt.Errorf("entry %d retries %d, which isn't an earlier bid by the same bidder", i, *of)
			}
		}
		last = e.OffsetUS
	}
	return nil
}

// Save writes the schedule as JSON.
func (s Schedule) Save(path string) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Load reads and validates a saved schedule, so the same inputs can be sent again.
func Load(path string) (Schedule, error) {
	var s Schedule
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("read schedule: %w", err)
	}
	return s, s.Validate()
}
