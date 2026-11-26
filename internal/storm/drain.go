package storm

import (
	"context"
	"time"

	"github.com/djwirz/bid-storm/internal/record"
)

// Drain waits until every condition returns "", for up to timeout. A condition otherwise
// says what it is still waiting for; Drain returns those that never finished.
func Drain(ctx context.Context, timeout time.Duration, conditions ...func() string) []string {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		waiting := []string{}
		for _, c := range conditions {
			if w := c(); w != "" {
				waiting = append(waiting, w)
			}
		}
		if len(waiting) == 0 {
			return waiting
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return waiting
		}
	}
}

// CheckTimeline looks for stages missing from the recording. accepted maps bid ids to
// amounts. held is nil when holds weren't recorded; announced has one set per subscriber.
// A stream event names no bid, but accepted amounts strictly increase within an auction, so
// the amount identifies one.
func CheckTimeline(accepted map[string]int64, held map[string]bool, announced []map[int64]bool) record.Timeline {
	var t record.Timeline
	claimed := map[int64]bool{}
	for id, amount := range accepted {
		claimed[amount] = true
		if held != nil && !held[id] {
			t.AcceptedWithoutHold++
		}
		for _, seen := range announced {
			if !seen[amount] {
				t.AcceptedUnannounced++
				break
			}
		}
	}
	unclaimed := map[int64]bool{}
	for _, seen := range announced {
		for amount := range seen {
			if !claimed[amount] {
				unclaimed[amount] = true
			}
		}
	}
	t.AnnouncedUnclaimed = len(unclaimed)
	return t
}
