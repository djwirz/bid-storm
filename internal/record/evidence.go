package record

import (
	"encoding/json"
	"os"
)

// The end-of-run evidence (spec §3, ADR 0008): what the fake and the target's own checker
// say once the storm is over. Like events, only the fields named here are written.

// Audit is audit.json: the intents the Stripe fake holds for the run's auction.
type Audit struct {
	V         int      `json:"v"`
	AuctionID string   `json:"auction_id"`
	FetchedUS int64    `json:"fetched_us"`
	Intents   []Intent `json:"intents"`
}

// Intent is one hold as the fake knows it.
type Intent struct {
	IntentID         string `json:"intent_id"`
	BidID            string `json:"bid_id"`
	Amount           int64  `json:"amount"`
	AmountCapturable int64  `json:"amount_capturable"`
	Status           string `json:"status"` // requires_capture, canceled or succeeded
}

// Check is check.json: the verdict of the target's own checker. It passed if ExitCode is 0.
type Check struct {
	V             int         `json:"v"`
	AuctionID     string      `json:"auction_id"`
	ExitCode      int         `json:"exit_code"`
	Quiet         bool        `json:"quiet"` // the target finished its follow-up work in time
	Status        string      `json:"status"`
	BidsAccepted  int         `json:"bids_accepted"`
	LostRace      int         `json:"lost_race"`
	Rejected      int         `json:"rejected"`
	Won           int         `json:"won"`
	Holds         int         `json:"holds"`
	HoldsReleased int         `json:"holds_released"`
	HoldsCaptured int         `json:"holds_captured"`
	HoldsLive     int         `json:"holds_live"`
	OutboxFailed  int         `json:"outbox_failed"`
	Violations    []Violation `json:"violations"`
}

// Violation is one broken invariant, in the checker's numbering.
type Violation struct {
	Invariant int     `json:"invariant"`
	BidID     *string `json:"bid_id"`
	// Message is the checker's own sentence, clipped: ids, amounts and states.
	Message string `json:"message"`
}

// SaveJSON writes v as indented JSON, replacing any earlier file.
func SaveJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
