package storm

import (
	"fmt"
	"time"

	"github.com/djwirz/bid-storm/internal/record"
)

// InvalidReasons says why a run can't be trusted as a measurement (spec §5). Empty means it can.
func InvalidReasons(res record.Results, lagLimit time.Duration, runErr, writeErr error) []string {
	reasons := []string{}
	if lag := time.Duration(res.SendLagMaxUS) * time.Microsecond; lag > lagLimit {
		reasons = append(reasons, fmt.Sprintf("generator fell behind: a send started %s late (limit %s)", lag, lagLimit))
	}
	if res.Skipped > 0 {
		reasons = append(reasons, fmt.Sprintf("%d sends skipped at the in-flight cap", res.Skipped))
	}
	if res.AcceptedBids > 0 && res.HoldCalls != nil && res.HoldCalls["authorize"] == 0 {
		reasons = append(reasons, "bids were accepted but the proxy saw no authorize: is the target's FAKE_PAYMENTS_URL pointed at it?")
	}
	if n := res.Timeline.AcceptedWithoutHold; n > 0 {
		reasons = append(reasons, fmt.Sprintf("%d accepted bids have no successful hold recorded", n))
	}
	if n := res.Timeline.AcceptedUnannounced; n > 0 {
		reasons = append(reasons, fmt.Sprintf("%d accepted bids did not reach every subscriber", n))
	}
	if n := res.Timeline.AnnouncedUnclaimed; n > 0 {
		reasons = append(reasons, fmt.Sprintf("%d streamed bids match no recorded accepted bid", n))
	}
	reasons = append(reasons, res.DrainWaiting...)
	if runErr != nil {
		reasons = append(reasons, "the run stopped early")
	}
	if writeErr != nil {
		reasons = append(reasons, "events could not all be written")
	}
	return reasons
}
