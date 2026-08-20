package offers_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeustest"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/money"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/offers"
)

// The figures here are a live capture: offer Q9BDY30Z9A at RTPARADG quoted
// 988.30 EUR, re-fetched by ID, with Amadeus supplying the tögrög rate beside
// it. 988.30 x 4161.336 rounds to 4112648 MNT at the currency's zero decimal
// places.
const (
	byIDOfferID  = "Q9BDY30Z9A"
	byIDEurTotal = "988.30"
	byIDMntTotal = "4112648"

	// money.Amount renders canonically, dropping insignificant trailing zeros,
	// so the wire's "988.30" displays as "988.3". The two spellings are the
	// same amount; keeping both names apart stops the tests asserting that the
	// SDK preserves a formatting detail it deliberately does not.
	byIDEurShown = "988.3 EUR"
	byIDMntShown = byIDMntTotal + " MNT"
)

// byIDResponse is what the by-ID endpoint returns for an offer that was found
// by a search asking for MNT.
//
// The dictionaries block is the point. GetQuery sends no currency parameter -
// it carries only lang - yet Amadeus attaches the conversion rate anyway,
// because the context follows the offer ID. Verified against the live sandbox.
const byIDResponse = `{
  "data": {
    "type":"hotel-offers",
    "hotel":{"hotelId":"RTPARADG","name":"Aparthotel Adagio Paris"},
    "available":true,
    "offers":[{"id":"` + byIDOfferID + `","roomQuantity":4,
               "guests":{"adults":4},
               "price":{"currency":"EUR","total":"` + byIDEurTotal + `"}}]
  },
  "dictionaries": {
    "currencyConversionLookupRates": {
      "EUR": {"rate":"4161.3360000000002401","target":"MNT","targetDecimalPlaces":0}
    }
  }
}`

func getByID(t *testing.T) *offers.OfferDetail {
	t.Helper()

	server := amadeustest.New(t)
	server.JSON(http.MethodGet, searchPath+"/"+byIDOfferID, http.StatusOK, byIDResponse)

	detail, err := offers.NewService(server.Client()).Get(
		context.Background(), offers.GetQuery{OfferID: byIDOfferID})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return detail
}

// TestOfferDetailCarriesTheConversionRates is the re-verify path's equivalent
// of the guarantee HotelOffers already makes.
//
// Re-fetching an offer by ID immediately before booking is where the price
// shown to a guest is settled, so it is the last place that should lose the
// rate needed to show it. Amadeus sends the rate on this response; dropping it
// in the mapper forces a caller to carry rates over from the earlier search by
// hand, and a caller who forgets shows euros to somebody expecting tögrög.
func TestOfferDetailCarriesTheConversionRates(t *testing.T) {
	detail := getByID(t)

	target, ok := detail.Rates.Target()
	if !ok {
		t.Fatal("the by-ID response carried a conversion rate, but OfferDetail dropped it")
	}
	if target != money.Currency("MNT") {
		t.Errorf("rates convert to %s, want MNT", target)
	}

	converted, err := detail.Rates.Convert(detail.Offer.Price.Total)
	if err != nil {
		t.Fatalf("converting the re-verified total: %v", err)
	}
	if got := converted.String(); got != byIDMntShown {
		t.Errorf("converted total = %s, want %s", got, byIDMntShown)
	}
}

// TestOfferDetailDisplayTotalConverts checks the convenience the caller
// actually reaches for, so that the by-ID path reads the same as the search
// path at the call site.
func TestOfferDetailDisplayTotalConverts(t *testing.T) {
	detail := getByID(t)

	shown := detail.DisplayTotal()
	if !shown.Converted {
		t.Fatalf("DisplayTotal did not convert: %s", shown)
	}
	if got := shown.String(); got != byIDMntShown {
		t.Errorf("DisplayTotal = %s, want %s", got, byIDMntShown)
	}

	// The original must survive untouched: it is what the booking is charged,
	// and a converted estimate must never be mistaken for it.
	if got := shown.Original.String(); got != byIDEurShown {
		t.Errorf("Original = %s, want %s", got, byIDEurShown)
	}
}

// TestOfferDetailWithoutRatesDisplaysTheOriginal covers the ordinary case: a
// search that asked for no currency gets no rates, and the display amount must
// then be the quoted price rather than nothing at all.
func TestOfferDetailWithoutRatesDisplaysTheOriginal(t *testing.T) {
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, searchPath+"/"+byIDOfferID, http.StatusOK, `{
	  "data": {
	    "type":"hotel-offers",
	    "hotel":{"hotelId":"RTPARADG","name":"Aparthotel Adagio Paris"},
	    "available":true,
	    "offers":[{"id":"`+byIDOfferID+`",
	               "price":{"currency":"EUR","total":"`+byIDEurTotal+`"}}]
	  }
	}`)

	detail, err := offers.NewService(server.Client()).Get(
		context.Background(), offers.GetQuery{OfferID: byIDOfferID})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if detail.Rates != nil {
		t.Errorf("Rates = %v, want nil when the response carried none", detail.Rates)
	}

	shown := detail.DisplayTotal()
	if shown.Converted {
		t.Error("DisplayTotal reported a conversion with no rate available")
	}
	if got := shown.String(); got != byIDEurShown {
		t.Errorf("DisplayTotal = %s, want the original %s", got, byIDEurShown)
	}
}
