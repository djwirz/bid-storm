package record

import (
	"encoding/json"
	"os"

	"github.com/djwirz/bid-storm/internal/schedule"
)

// Manifest says what produced a recording (spec §3). It holds no hostnames, user names or paths.
type Manifest struct {
	SchemaVersion  int             `json:"schema_version"`
	RunID          string          `json:"run_id"`
	StartedUS      int64           `json:"started_us"`
	EndedUS        int64           `json:"ended_us"`
	Complete       bool            `json:"complete"` // the whole schedule ran and the events were saved
	Valid          bool            `json:"valid"`    // complete, and the generator kept up (spec §5)
	InvalidReasons []string        `json:"invalid_reasons"`
	Schedule       schedule.Params `json:"schedule"`
	ScheduleFrom   string          `json:"schedule_from"` // "generated" or "file"
	Settings       Settings        `json:"settings"`
	Target         Target          `json:"target"`
	Machine        Machine         `json:"machine"`
	Generator      Generator       `json:"generator"`
	Results        Results         `json:"results"`
}

// Settings are the runner's limits and intervals.
type Settings struct {
	AuctionSeconds   int   `json:"auction_seconds"`
	LagLimitUS       int64 `json:"lag_limit_us"`
	MaxInFlight      int   `json:"max_in_flight"`
	PriceEveryUS     int64 `json:"price_every_us"`
	PollEveryUS      int64 `json:"poll_every_us"`
	PollTimeoutUS    int64 `json:"poll_timeout_us"`
	RequestTimeoutUS int64 `json:"request_timeout_us"`
}

// Target describes what was stormed.
type Target struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	AuctionID string `json:"auction_id"`
	// Commit is the target checkout's HEAD, which is assumed to be what was running.
	Commit      string `json:"commit"`
	CommitDirty bool   `json:"commit_dirty"`
	// FakeDeclared is what the operator says the Stripe fake was started with, not observed.
	FakeDeclared  map[string]string `json:"fake_settings_declared"`
	InstancesSeen []string          `json:"instances_seen"` // observed in replies
}

// Machine is where the generator ran.
type Machine struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	CPUs int    `json:"cpus"`
}

// Generator is the build of this program.
type Generator struct {
	GoVersion string `json:"go_version"`
	Revision  string `json:"revision"`
	Dirty     bool   `json:"dirty"`
}

// Results summarize the run. The events are the record; these are for a quick look.
type Results struct {
	Scheduled       int            `json:"scheduled"`
	Sent            int            `json:"sent"`
	Skipped         int            `json:"skipped"`
	SendLagMaxUS    int64          `json:"send_lag_max_us"`
	SendLagP99US    int64          `json:"send_lag_p99_us"`
	HTTPStatuses    map[string]int `json:"http_statuses"` // of bid POSTs
	TransportErrors map[string]int `json:"transport_errors"`
	// LastStatuses counts distinct bids by the last status a reply showed, with the reason if
	// any. Not the final state: an accepted bid is rarely read again after being outbid.
	LastStatuses    map[string]int `json:"bids_by_last_status"`
	AcceptedBids    int            `json:"accepted_bids"` // distinct bids ever seen accepted
	Polls           int            `json:"polls"`
	UnresolvedBids  int            `json:"unresolved_bids"` // still pending when polling gave up
	PriceReadErrors int            `json:"price_read_errors"`
}

// Save writes the manifest, replacing any earlier one.
func (m Manifest) Save(path string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
