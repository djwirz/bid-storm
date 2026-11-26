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
	if runErr != nil {
		reasons = append(reasons, "the run stopped early")
	}
	if writeErr != nil {
		reasons = append(reasons, "events could not all be written")
	}
	return reasons
}
