//go:build live

package livetest

import (
	"errors"
	"testing"

	sdk "github.com/techpartners-asia/amadeus-hotel-integration/v2"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/offers"
)

// conversionCurrency is the currency the conversion tests ask for.
//
// MNT is deliberate, matching internal/capture: the tögrög has no minor unit,
// so a converted amount must be rounded to whole units. A target with two
// decimal places would let a rounding mistake pass unnoticed.
const conversionCurrency = "MNT"

// TestOfferByIDCarriesConversionRatesLive checks against the real API that the
// re-verify step can still show the guest the currency they were quoted in.
//
// The offline test for this is built from a captured response, which proves the
// mapping but not the premise. The premise is Amadeus's: that a by-ID call,
// which takes no currency parameter of its own, nevertheless returns the
// conversion rate for an offer that was found by a search asking for one. This
// is the test that would notice if that stopped being true.
func TestOfferByIDCarriesConversionRatesLive(t *testing.T) {
	client := newClient(t)
	ctx := testContext(t)

	stay := stayDates()

	var found offers.Offer
	var searchRates offers.ConversionRates

	for _, city := range searchCities() {
		for _, batch := range batches(pricableHotelIDs(t, ctx, client, city), 20) {
			results, err := client.Offers.Search(ctx, offers.SearchQuery{
				HotelIDs:     batch,
				Stay:         stay,
				Guests:       offers.Guests{Adults: requestedAdults},
				Rooms:        requestedRooms,
				Currency:     conversionCurrency,
				BestRateOnly: codes.Ptr(false),
			})
			if err != nil {
				continue
			}
			for _, result := range results {
				if len(result.Offers) > 0 && len(result.Rates) > 0 {
					found, searchRates = result.Offers[0], result.Rates
					break
				}
			}
			if searchRates != nil {
				break
			}
		}
		if searchRates != nil {
			break
		}
	}

	if searchRates == nil {
		t.Skipf("no priced offer came back with a %s conversion rate", conversionCurrency)
	}

	target, _ := searchRates.Target()
	t.Logf("search: offer %s quoted %s, rates convert to %s", found.ID, found.Price.Total, target)

	detail, err := client.Offers.Get(ctx, offers.GetQuery{OfferID: found.ID})
	if err != nil {
		if errors.Is(err, sdk.ErrNotFound) {
			t.Skipf("offer %s expired before it could be re-verified", found.ID)
		}
		t.Fatalf("Offers.Get: %v", err)
	}

	if len(detail.Rates) == 0 {
		t.Fatalf("the by-ID response for %s carried no conversion rates, so a caller "+
			"cannot show the price in %s at the point of booking", found.ID, conversionCurrency)
	}

	detailTarget, ok := detail.Rates.Target()
	if !ok || detailTarget != target {
		t.Errorf("by-ID rates convert to %q, want %q", detailTarget, target)
	}

	shown := detail.DisplayTotal()
	if !shown.Converted {
		t.Errorf("DisplayTotal did not convert %s", shown.Original)
	}
	if shown.Currency() != target {
		t.Errorf("DisplayTotal is in %s, want %s", shown.Currency(), target)
	}
	t.Logf("by ID: %s charged, %s shown", shown.Original, shown)
}
