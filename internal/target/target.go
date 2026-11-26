// Package target is the seam between the generator and a particular system under test: what
// the shared contract doesn't cover, and the guards on what may be loaded at all.
package target

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/djwirz/bid-storm/internal/record"
)

// Adapter covers what differs per target (spec §2).
type Adapter interface {
	Name() string
	// Preflight returns an error unless the target is safe to storm.
	Preflight(ctx context.Context) error
	// CreateAuction opens an auction that ends after length and returns its id.
	CreateAuction(ctx context.Context, title string, length time.Duration) (string, error)
	// Revision is the target's source commit, and whether it has uncommitted changes.
	Revision(ctx context.Context) (commit string, dirty bool, err error)
	// InstanceHeader names the reply header identifying the instance that answered, or "".
	InstanceHeader() string
	// Check runs the target's own checker on an auction, against its database and the fake.
	// Violations are part of the verdict; an error means there is no verdict.
	Check(ctx context.Context, auctionID, fakeURL string) (record.Check, error)
}

// CheckLoopback returns an error unless rawURL points at this machine. Version one loads
// only local deployments, so nothing else is accepted.
func CheckLoopback(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("not a URL")
	}
	host := u.Hostname()
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("host %q is not this machine: only localhost, 127.0.0.0/8 and ::1 are allowed", host)
}
