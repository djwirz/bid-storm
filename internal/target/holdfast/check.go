package holdfast

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/djwirz/bid-storm/internal/contract"
	"github.com/djwirz/bid-storm/internal/record"
	"github.com/djwirz/bid-storm/internal/target"
)

// How long Holdfast's checker may wait for the auction to close and everything to settle.
const checkWait = 60 * time.Second

// Check implements target.Adapter with Holdfast's own server/src/check-invariants.ts, which
// reads the database and the fake. A violation is a verdict, not an error: an error means
// there is no verdict.
func (a *Adapter) Check(ctx context.Context, auctionID, fakeURL string) (record.Check, error) {
	var none record.Check
	if err := target.CheckLoopback(fakeURL); err != nil {
		return none, fmt.Errorf("fake: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, checkWait+60*time.Second)
	defer cancel()
	tmp, err := os.MkdirTemp("", "bidstorm-check-*")
	if err != nil {
		return none, err
	}
	defer os.RemoveAll(tmp)
	out := filepath.Join(tmp, "check.json")

	cmd := exec.CommandContext(ctx, a.node, "src/check-invariants.ts",
		"--auction", auctionID, "--fake", fakeURL, "--out", out,
		"--wait", strconv.Itoa(int(checkWait.Seconds())))
	cmd.Dir = filepath.Join(a.checkout, "server")
	cmd.Env = a.scriptEnv()
	var output strings.Builder
	cmd.Stdout, cmd.Stderr = &output, &output
	runErr := cmd.Run()
	// The script exits non-zero to report violations, so that alone isn't a failure here.
	var exit *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exit) {
		return none, fmt.Errorf("check-invariants.ts did not run: %w", runErr)
	}
	// The output is shown, never saved: an error text could hold anything.
	raw, err := os.ReadFile(out)
	if err != nil {
		return none, fmt.Errorf("check-invariants.ts wrote no verdict (exit %d)\n%s", cmd.ProcessState.ExitCode(), output.String())
	}
	check, err := parseCheck(raw)
	if err != nil {
		return none, err
	}
	if check.AuctionID != auctionID {
		return none, errors.New("check-invariants.ts judged a different auction")
	}
	check.ExitCode = cmd.ProcessState.ExitCode() // -1 if it was killed
	return check, nil
}

// parseCheck copies the named fields of the checker's summary.
func parseCheck(raw []byte) (record.Check, error) {
	// Pointers where reading a missing field as zero would look like a clean verdict.
	var s struct {
		AuctionID     string `json:"auctionId"`
		Quiet         *bool  `json:"quiet"`
		Status        string `json:"status"`
		BidsAccepted  int    `json:"bidsAccepted"`
		LostRace      int    `json:"lostRace"`
		Rejected      int    `json:"rejected"`
		Won           int    `json:"won"`
		Holds         int    `json:"holds"`
		HoldsReleased int    `json:"holdsReleased"`
		HoldsCaptured int    `json:"holdsCaptured"`
		HoldsLive     int    `json:"holdsLive"`
		OutboxFailed  *int   `json:"outboxFailed"`
		Violations    *[]struct {
			Invariant int    `json:"invariant"`
			BidID     string `json:"bidId"`
			Message   string `json:"message"`
		} `json:"violations"`
	}
	if err := json.Unmarshal(raw, &s); err != nil || s.Quiet == nil || s.OutboxFailed == nil || s.Violations == nil {
		return record.Check{}, errors.New("check-invariants.ts wrote a summary this adapter doesn't understand")
	}
	c := record.Check{
		V: record.SchemaVersion, AuctionID: s.AuctionID, Quiet: *s.Quiet, Status: contract.Clip(s.Status),
		BidsAccepted: s.BidsAccepted, LostRace: s.LostRace, Rejected: s.Rejected, Won: s.Won,
		Holds: s.Holds, HoldsReleased: s.HoldsReleased, HoldsCaptured: s.HoldsCaptured, HoldsLive: s.HoldsLive,
		OutboxFailed: *s.OutboxFailed, Violations: []record.Violation{},
	}
	for _, v := range *s.Violations {
		violation := record.Violation{Invariant: v.Invariant, Message: contract.Clip(v.Message)}
		if v.BidID != "" {
			id := contract.Clip(v.BidID)
			violation.BidID = &id
		}
		c.Violations = append(c.Violations, violation)
	}
	return c, nil
}
