package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/djwirz/bid-storm/internal/contract"
	"github.com/djwirz/bid-storm/internal/record"
	"github.com/djwirz/bid-storm/internal/target"
)

// Audit asks the fake itself, not through the proxy, for every intent it holds and keeps
// those of one auction. The fake is shared, so it also lists other runs' intents.
func Audit(ctx context.Context, fakeURL, auctionID string) ([]record.Intent, error) {
	if err := target.CheckLoopback(fakeURL); err != nil {
		return nil, fmt.Errorf("fake: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(fakeURL, "/")+"/audit", nil)
	if err != nil {
		return nil, err
	}
	res, err := contract.NewHTTPClient(30 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("fake audit: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fake audit: HTTP %d", res.StatusCode)
	}
	var body struct {
		// A pointer, so a reply without the list fails instead of reading as no holds.
		Intents *[]struct {
			ID               string `json:"id"`
			Amount           int64  `json:"amount"`
			AmountCapturable int64  `json:"amountCapturable"`
			Status           string `json:"status"`
			Metadata         struct {
				BidID     string `json:"bidId"`
				AuctionID string `json:"auctionId"`
			} `json:"metadata"`
		} `json:"intents"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64*maxBody)).Decode(&body); err != nil {
		return nil, fmt.Errorf("fake audit: %w", err)
	}
	if body.Intents == nil {
		return nil, errors.New("fake audit: no list of intents in the reply")
	}
	intents := []record.Intent{}
	for _, in := range *body.Intents {
		if in.Metadata.AuctionID != auctionID {
			continue
		}
		intents = append(intents, record.Intent{
			IntentID: contract.Clip(in.ID), BidID: contract.Clip(in.Metadata.BidID),
			Amount: in.Amount, AmountCapturable: in.AmountCapturable, Status: contract.Clip(in.Status),
		})
	}
	slices.SortFunc(intents, func(a, b record.Intent) int { return strings.Compare(a.IntentID, b.IntentID) })
	return intents, nil
}
