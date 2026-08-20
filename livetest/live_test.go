//go:build live

// Package livetest holds the end-to-end suite that talks to the real Amadeus
// sandbox.
//
// It is behind a build tag, so it never runs as part of `go test ./...`. The
// offline mapper tests in each context are the regression suite; this one
// exists to catch Amadeus changing its API underneath us, which no fixture can
// detect.
//
//	AMADEUS_CLIENT_ID=... AMADEUS_CLIENT_SECRET=... go test -tags live ./livetest/ -v
//
// Credentials come from the environment only. The previous suite carried
// sandbox credentials committed in the source, which is a habit worth not
// keeping: committed credentials get copied into places that are not sandboxes.
package livetest

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	sdk "github.com/techpartners-asia/amadeus-hotel-integration/v2"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/content"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/datetime"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/geo"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/inventory"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/offers"
)

// newClient builds a client from the environment, skipping the test when no
// credentials are available.
func newClient(t *testing.T) *sdk.Client {
	t.Helper()
	return newClientWithTransport(t, nil)
}

// newClientWithTransport is newClient with the round tripper replaced, for the
// one test that needs to see the raw traffic. A nil transport leaves the SDK
// to build its own client, which is what every other test wants.
func newClientWithTransport(t *testing.T, transport http.RoundTripper) *sdk.Client {
	t.Helper()

	id, secret := os.Getenv("AMADEUS_CLIENT_ID"), os.Getenv("AMADEUS_CLIENT_SECRET")
	if id == "" || secret == "" {
		t.Skip("set AMADEUS_CLIENT_ID and AMADEUS_CLIENT_SECRET to run the live suite")
	}

	cfg := sdk.Config{
		ClientID:     id,
		ClientSecret: secret,
		Environment:  environment(t),
	}
	if transport != nil {
		// A supplied client keeps its own timeout, so Config.Timeout would be
		// ignored; set one here to match the SDK's own generous default.
		cfg.HTTPClient = &http.Client{Transport: transport, Timeout: 90 * time.Second}
	}

	client, err := sdk.New(cfg)
	if err != nil {
		t.Fatalf("authenticating: %v", err)
	}
	return client
}

// environment selects the deployment to run against, defaulting to the
// sandbox.
//
// Production is opt-in through AMADEUS_ENVIRONMENT because some behaviour only
// appears there: the sandbox holds a static subset of inventory, so a provider
// quirk seen in production may be unreproducible against Test. It is safe for
// this suite as it stands - nothing here creates an order - but a live suite
// pointed at production must never grow one without saying so.
func environment(t *testing.T) sdk.Environment {
	t.Helper()

	switch value := strings.ToLower(strings.TrimSpace(os.Getenv("AMADEUS_ENVIRONMENT"))); value {
	case "", "test":
		return sdk.Test
	case "production", "prod":
		t.Log("running against PRODUCTION")
		return sdk.Production
	default:
		t.Fatalf("AMADEUS_ENVIRONMENT=%q is not test or production", value)
		return sdk.Test
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// stayDates returns a check-in a month out and a three-night stay, so the
// sandbox has inventory and the dates are never in the past.
func stayDates() offers.Stay {
	checkIn := datetime.Today(time.UTC).AddDays(30)
	return offers.Stay{CheckIn: checkIn, CheckOut: checkIn.AddDays(3)}
}

func TestPing(t *testing.T) {
	client := newClient(t)
	if err := client.Ping(testContext(t)); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestInventoryByCity(t *testing.T) {
	client := newClient(t)

	hotels, err := client.Inventory.ByCity(testContext(t), inventory.CityQuery{
		CityCode: "PAR",
		Filters:  inventory.Filters{Radius: 5},
	})
	if err != nil {
		t.Fatalf("ByCity: %v", err)
	}
	if len(hotels) == 0 {
		t.Fatal("no hotels returned for PAR")
	}

	// Whatever else changes, a hotel must have an ID and a name.
	for _, hotel := range hotels[:min(5, len(hotels))] {
		if hotel.ID == "" || hotel.Name == "" {
			t.Errorf("incompletely mapped hotel: %+v", hotel)
		}
	}
	t.Logf("mapped %d hotels; first: %s (%s)", len(hotels), hotels[0].Name, hotels[0].ID)
}

func TestInventoryByGeocode(t *testing.T) {
	client := newClient(t)

	hotels, err := client.Inventory.ByGeocode(testContext(t), inventory.GeocodeQuery{
		Position: geo.Coordinates{Latitude: 48.8566, Longitude: 2.3522},
		Filters:  inventory.Filters{Radius: 5},
	})
	if err != nil {
		t.Fatalf("ByGeocode: %v", err)
	}
	if len(hotels) == 0 {
		t.Fatal("no hotels returned around Paris")
	}
}

func TestInventoryByKeyword(t *testing.T) {
	// Hotel Name Autocomplete is its own product on the Enterprise gateway, so
	// this test is also the check that a subscription carries it. Missing
	// entitlement surfaces as a 401 with Amadeus error 38190 ("Invalid access
	// token") on this endpoint alone, while the same token passes Hotel List.
	client := newClient(t)

	suggestions, err := client.Inventory.ByKeyword(testContext(t), inventory.KeywordQuery{
		Keyword: "PARI",
	})
	if err != nil {
		t.Fatalf("ByKeyword: %v", err)
	}
	if len(suggestions) == 0 {
		t.Fatal("no suggestions returned for PARI")
	}

	for _, suggestion := range suggestions {
		if suggestion.Name == "" || len(suggestion.HotelIDs) == 0 {
			t.Errorf("incompletely mapped suggestion: %+v", suggestion)
		}
	}
	t.Logf("mapped %d suggestions; first: %s %v",
		len(suggestions), suggestions[0].Name, suggestions[0].HotelIDs)
}

func TestContentForARealProperty(t *testing.T) {
	client := newClient(t)
	ctx := testContext(t)

	hotels, err := client.Inventory.ByCity(ctx, inventory.CityQuery{CityCode: "PAR"})
	if err != nil {
		t.Fatalf("ByCity: %v", err)
	}
	if len(hotels) == 0 {
		t.Skip("no hotels to fetch content for")
	}

	hotel, err := client.Content.Get(ctx, content.Query{HotelID: hotels[0].ID.String()})
	if err != nil {
		t.Fatalf("Content.Get: %v", err)
	}
	if hotel.ID == "" {
		t.Errorf("content came back unmapped: %+v", hotel)
	}
	t.Logf("%s: %d rooms, %d amenities, %d photos",
		hotel.Name, len(hotel.Rooms), len(hotel.Amenities), len(hotel.Media))
}

func TestOffersSearchAndGroup(t *testing.T) {
	client := newClient(t)
	ctx := testContext(t)

	hotels, err := client.Inventory.ByCity(ctx, inventory.CityQuery{
		CityCode: "PAR",
		Filters:  inventory.Filters{Source: codes.HotelSourceDirectChain},
	})
	if err != nil {
		t.Fatalf("ByCity: %v", err)
	}
	if len(hotels) == 0 {
		t.Skip("no hotels to price")
	}

	ids := inventory.IDs(hotels)
	if len(ids) > 20 {
		ids = ids[:20]
	}

	results, err := client.Offers.Search(ctx, offers.SearchQuery{
		HotelIDs:     ids,
		Stay:         stayDates(),
		Guests:       offers.Guests{Adults: 2},
		BestRateOnly: codes.Ptr(false),
	})
	if err != nil {
		t.Fatalf("Offers.Search: %v", err)
	}

	priced := 0
	for _, result := range results {
		for _, offer := range result.Offers {
			if offer.Price.Total.Amount().IsZero() {
				t.Errorf("offer %s came back with no usable price", offer.ID)
				continue
			}
			if offer.Price.Currency == "" {
				t.Errorf("offer %s has an amount but no currency", offer.ID)
			}
			priced++
		}
	}
	if priced == 0 {
		t.Skip("the sandbox returned no priced offers for these dates")
	}
	t.Logf("mapped %d priced offers across %d hotels", priced, len(results))

	// Grouping must not lose or duplicate an offer.
	for _, result := range results {
		grouped := 0
		for _, room := range result.GroupByRoom() {
			grouped += len(room.Offers)
		}
		if grouped != len(result.Offers) {
			t.Errorf("%s: grouping produced %d offers from %d",
				result.Hotel.ID, grouped, len(result.Offers))
		}
	}
}

func TestOfferByIDRoundTrip(t *testing.T) {
	client := newClient(t)
	ctx := testContext(t)

	hotels, err := client.Inventory.ByCity(ctx, inventory.CityQuery{CityCode: "PAR"})
	if err != nil || len(hotels) == 0 {
		t.Skip("no hotels available")
	}

	ids := inventory.IDs(hotels)
	if len(ids) > 20 {
		ids = ids[:20]
	}

	results, err := client.Offers.Search(ctx, offers.SearchQuery{
		HotelIDs: ids,
		Stay:     stayDates(),
		Guests:   offers.Guests{Adults: 2},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	var offerID offers.OfferID
	for _, result := range results {
		if len(result.Offers) > 0 {
			offerID = result.Offers[0].ID
			break
		}
	}
	if offerID == "" {
		t.Skip("no offers to re-fetch")
	}

	detail, err := client.Offers.Get(ctx, offers.GetQuery{OfferID: offerID})
	if err != nil {
		// An offer expiring between the two calls is normal, not a failure.
		if errors.Is(err, sdk.ErrNotFound) {
			t.Skipf("offer %s expired before it could be re-fetched", offerID)
		}
		t.Fatalf("Offers.Get: %v", err)
	}
	if detail.Offer.ID != offerID {
		t.Errorf("got offer %s, want %s", detail.Offer.ID, offerID)
	}
	if detail.Hotel.ID == "" {
		t.Error("the hotel was dropped from the by-ID response")
	}
}

func TestValidationStillRejectsLocally(t *testing.T) {
	// The local checks must agree with the live API about what is invalid.
	client := newClient(t)
	ctx := testContext(t)

	if _, err := client.Inventory.ByCity(ctx, inventory.CityQuery{}); !errors.Is(err, sdk.ErrValidation) {
		t.Errorf("empty city query = %v, want ErrValidation", err)
	}
	if _, err := client.Offers.Search(ctx, offers.SearchQuery{}); !errors.Is(err, sdk.ErrValidation) {
		t.Errorf("empty search = %v, want ErrValidation", err)
	}
}

func TestUnknownHotelIsNotFound(t *testing.T) {
	client := newClient(t)

	_, err := client.Content.Get(testContext(t), content.Query{HotelID: "ZZZZ9999"})
	if err == nil {
		t.Fatal("expected an error for an unknown property")
	}
	// Amadeus is inconsistent about whether this is a 404 or a 400 with an
	// error envelope, so accept either rather than asserting a specific one.
	if !errors.Is(err, sdk.ErrNotFound) && !errors.Is(err, sdk.ErrInvalidRequest) {
		t.Errorf("err = %v, want not-found or invalid-request", err)
	}
}
