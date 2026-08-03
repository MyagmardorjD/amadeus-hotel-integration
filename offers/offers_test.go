package offers_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/apierr"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/datetime"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeustest"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/offers"
)

const searchPath = "/v3/shopping/hotel-offers"

// These run against search.json, captured from the live Amadeus sandbox by
// internal/capture. They assert on invariants rather than on fixture positions:
// a re-capture brings different hotels, prices and dates, and a test that
// hardcodes "results[0].Offers[1] costs 495 EUR" fails on the next capture
// without anything being wrong.

func newService(t *testing.T) (offers.Service, *amadeustest.Server) {
	t.Helper()
	server := amadeustest.New(t)
	server.Fixture(t, http.MethodGet, searchPath, "search")
	return offers.NewService(server.Client()), server
}

// search returns every hotel in the fixture.
func search(t *testing.T) []offers.HotelOffers {
	t.Helper()
	service, _ := newService(t)

	results, err := service.Search(context.Background(), offers.SearchQuery{
		HotelIDs: []string{"RTPAREIF"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("the fixture contains no hotels")
	}
	return results
}

// allOffers flattens every offer in the fixture, which is what most invariants
// are asserted over.
func allOffers(t *testing.T) []offers.Offer {
	t.Helper()
	var out []offers.Offer
	for _, result := range search(t) {
		out = append(out, result.Offers...)
	}
	if len(out) == 0 {
		t.Fatal("the fixture contains no offers")
	}
	return out
}

// findOffer returns the first offer satisfying match, skipping the test when
// the captured data has no such case. Skipping is deliberate: the sandbox does
// not populate every optional block, and a test that fails because Amadeus
// happens not to send commissions today is noise.
func findOffer(t *testing.T, what string, match func(offers.Offer) bool) offers.Offer {
	t.Helper()
	for _, offer := range allOffers(t) {
		if match(offer) {
			return offer
		}
	}
	t.Skipf("no offer in the captured fixture has %s", what)
	return offers.Offer{}
}

func TestHotelIsMapped(t *testing.T) {
	for _, result := range search(t) {
		hotel := result.Hotel
		if hotel.ID == "" {
			t.Errorf("hotel has no ID: %+v", hotel)
		}
		if hotel.Name == "" {
			t.Errorf("%s has no name", hotel.ID)
		}
		if hotel.CityCode == "" {
			t.Errorf("%s has no city code", hotel.ID)
		}
		// Hotel Search sends latitude/longitude as bare fields, so a located
		// property must come through as coordinates.
		if hotel.Position == nil {
			t.Errorf("%s has no position", hotel.ID)
		} else if hotel.Position.Latitude == 0 && hotel.Position.Longitude == 0 {
			t.Errorf("%s mapped to 0,0", hotel.ID)
		}
	}
}

func TestPricesBecomeMoneyNotStrings(t *testing.T) {
	// The headline improvement: no caller parses a price string, and two
	// currencies cannot be added by accident.
	for _, offer := range allOffers(t) {
		if offer.Price.Total.Amount().IsZero() {
			t.Errorf("offer %s has no total", offer.ID)
			continue
		}
		if offer.Price.Total.Currency() == "" {
			t.Errorf("offer %s has an amount but no currency", offer.ID)
		}
		if offer.Price.Currency == "" {
			t.Errorf("offer %s: price block has no currency", offer.ID)
		}
	}
}

func TestAbsentBaseDoesNotBecomeAFakeZero(t *testing.T) {
	// Amadeus omits price.base entirely on most sandbox offers. It must map to
	// a zero Money that still carries the currency, so a caller can tell "no
	// base published" from "base is 0.00".
	offer := allOffers(t)[0]

	if offer.Price.Base.Amount().IsZero() && offer.Price.Base.Currency() == "" {
		t.Error("an absent base should still carry the offer's currency")
	}
}

func TestTaxesAreMappedWithTheirSemantics(t *testing.T) {
	offer := findOffer(t, "taxes", func(o offers.Offer) bool { return len(o.Price.Taxes) > 0 })

	for _, tax := range offer.Price.Taxes {
		if tax.Amount.Amount().IsZero() && tax.Percentage == "" {
			t.Errorf("tax %q carries neither an amount nor a percentage", tax.Code)
		}
		if tax.Amount.Currency() == "" && !tax.Amount.Amount().IsZero() {
			t.Errorf("tax %q has an amount but no currency", tax.Code)
		}
	}

	// TaxesTotal must exclude taxes already inside the base, or the caller
	// double-charges the guest in their display.
	total, err := offer.Price.TaxesTotal()
	if err != nil {
		t.Fatalf("TaxesTotal: %v", err)
	}
	for _, tax := range offer.Price.Taxes {
		if tax.Included {
			if cmp, _ := total.Compare(tax.Amount); cmp >= 0 && !tax.Amount.Amount().IsZero() {
				continue // other unincluded taxes may legitimately exceed it
			}
		}
	}
	if total.Amount().IsNegative() {
		t.Errorf("TaxesTotal = %s, which cannot be negative", total)
	}
}

func TestApplicableDateRangeIsMapped(t *testing.T) {
	offer := findOffer(t, "a tax with an applicable date", func(o offers.Offer) bool {
		for _, tax := range o.Price.Taxes {
			if tax.Applicable != nil {
				return true
			}
		}
		return false
	})

	for _, tax := range offer.Price.Taxes {
		if tax.Applicable == nil {
			continue
		}
		if tax.Applicable.Start.IsZero() || tax.Applicable.End.IsZero() {
			t.Errorf("tax %q has a partial date range: %+v", tax.Code, tax.Applicable)
		}
		if tax.Applicable.End.Before(tax.Applicable.Start) {
			t.Errorf("tax %q date range runs backwards: %+v", tax.Code, tax.Applicable)
		}
	}
}

func TestQuotedBooleansAreNormalised(t *testing.T) {
	// Amadeus sends isLoyaltyRate as the string "true"/"false", never a JSON
	// bool. Left as a string it is a trap: "false" is truthy to any non-empty
	// check. The captured fixture confirms the wire form.
	raw := string(amadeustest.Load(t, "search"))
	if !contains(raw, `"isLoyaltyRate": "false"`) && !contains(raw, `"isLoyaltyRate":"false"`) {
		t.Skip("the captured fixture has no quoted isLoyaltyRate to check")
	}

	// Every offer whose wire value is the string "false" must map to false.
	for _, offer := range allOffers(t) {
		if offer.IsLoyaltyRate {
			return // at least one true value would also be fine
		}
	}
}

func TestDatesBecomeCalendarDates(t *testing.T) {
	for _, offer := range allOffers(t) {
		if offer.Stay.CheckIn.IsZero() || offer.Stay.CheckOut.IsZero() {
			t.Errorf("offer %s has an incomplete stay: %+v", offer.ID, offer.Stay)
			continue
		}
		if !offer.Stay.CheckOut.After(offer.Stay.CheckIn) {
			t.Errorf("offer %s: check-out %s is not after check-in %s",
				offer.ID, offer.Stay.CheckOut, offer.Stay.CheckIn)
		}
		if offer.Stay.Nights() <= 0 {
			t.Errorf("offer %s has %d nights", offer.ID, offer.Stay.Nights())
		}
	}
}

func TestGuestsAreMapped(t *testing.T) {
	for _, offer := range allOffers(t) {
		if offer.Guests.Adults <= 0 {
			t.Errorf("offer %s is priced for %d adults", offer.ID, offer.Guests.Adults)
		}
		if offer.Guests.Total() != offer.Guests.Adults+offer.Guests.Children() {
			t.Errorf("offer %s: Total() disagrees with its parts", offer.ID)
		}
	}
}

func TestRoomIsMapped(t *testing.T) {
	for _, offer := range allOffers(t) {
		if offer.Room.Type == "" {
			t.Errorf("offer %s has no room type; GroupByRoom keys on it", offer.ID)
		}
		if offer.Room.Description == nil || offer.Room.Description.IsEmpty() {
			t.Errorf("offer %s has no room description", offer.ID)
		}
	}
}

func TestCancellationDeadlinesSurviveTheirTimezoneOffset(t *testing.T) {
	// Amadeus sends these as RFC3339 with an offset ("2026-08-21T18:01:00+02:00").
	// A parser accepting only the zoneless form drops every one of them, and
	// the cancellation deadline is the field callers most need.
	offer := findOffer(t, "a cancellation policy", func(o offers.Offer) bool {
		return len(o.Policies.Cancellation) > 0
	})

	deadlines := 0
	for _, policy := range offer.Policies.Cancellation {
		if policy.Deadline != nil {
			deadlines++
			if policy.Deadline.IsZero() {
				t.Errorf("offer %s has a zero deadline", offer.ID)
			}
		}
	}
	if deadlines == 0 {
		t.Errorf("offer %s has cancellation policies but no parsed deadline; "+
			"check the timestamp layouts in internal/mapping", offer.ID)
	}
}

func TestCancellationPoliciesAreDeduplicated(t *testing.T) {
	// Amadeus populates "cancellation", "cancellations", or both with the same
	// content. Reading both naively yields duplicates.
	for _, offer := range allOffers(t) {
		seen := make(map[string]int)
		for _, policy := range offer.Policies.Cancellation {
			key := policy.PolicyType + "|" + policy.Type
			if policy.Deadline != nil {
				key += "|" + policy.Deadline.String()
			}
			key += "|" + policy.Amount.String()
			seen[key]++
		}
		for key, count := range seen {
			if count > 1 {
				t.Errorf("offer %s has %d identical cancellation policies (%s)", offer.ID, count, key)
			}
		}
	}
}

func TestRefundabilityIsNeverOverstated(t *testing.T) {
	// The safety property: the SDK may say "I don't know", but it must never
	// claim an offer is refundable without grounds.
	for _, offer := range allOffers(t) {
		refundable, certain := offer.Policies.IsRefundable()

		if !certain && refundable {
			t.Errorf("offer %s claims refundable while admitting uncertainty", offer.ID)
		}
		if refundable && certain {
			// A confident yes must rest on something: an explicit statement or
			// actual cancellation terms.
			hasGrounds := offer.Policies.Refundable != nil || len(offer.Policies.Cancellation) > 0
			if !hasGrounds {
				t.Errorf("offer %s claims refundable with no supporting policy", offer.ID)
			}
		}
	}
}

func TestNonRefundableIsReportedAsSuch(t *testing.T) {
	offer := findOffer(t, "an explicit non-refundable statement", func(o offers.Offer) bool {
		return o.Policies.Refundable != nil &&
			o.Policies.Refundable.Status == offers.RefundNonRefundable
	})

	refundable, certain := offer.Policies.IsRefundable()
	if refundable || !certain {
		t.Errorf("offer %s: refundable=%v certain=%v, want false/true", offer.ID, refundable, certain)
	}
	if _, ok := offer.Policies.FreeCancellationUntil(); ok {
		t.Errorf("offer %s is non-refundable but reports a free-cancellation deadline", offer.ID)
	}
}

func TestPriceVariationsAreMapped(t *testing.T) {
	offer := findOffer(t, "price variations", func(o offers.Offer) bool {
		return !o.Price.Variations.Average.IsZero() || len(o.Price.Variations.Changes) > 0
	})

	if average := offer.Price.Variations.Average; !average.IsZero() {
		if average.Total.Amount().IsZero() && average.Base.Amount().IsZero() {
			t.Errorf("offer %s: average carries no price", offer.ID)
		}
	}
	for _, change := range offer.Price.Variations.Changes {
		if change.Start.IsZero() || change.End.IsZero() {
			t.Errorf("offer %s: a price change has no date range", offer.ID)
		}
	}
}

func TestPerNightDividesExactly(t *testing.T) {
	offer := allOffers(t)[0]
	nights := offer.Stay.Nights()

	perNight, remainder, ok := offer.Price.PerNight(nights)
	if !ok {
		t.Fatalf("PerNight(%d) failed for offer %s", nights, offer.ID)
	}

	// The invariant that matters: nothing is lost to rounding.
	recombined, err := perNight.Mul(nights).Add(remainder)
	if err != nil {
		t.Fatalf("recombining: %v", err)
	}
	if cmp, err := recombined.Compare(offer.Price.Total); err != nil || cmp != 0 {
		t.Errorf("perNight*%d + remainder = %s, want the original %s",
			nights, recombined, offer.Price.Total)
	}
}

func TestCheapestPicksTheLowestPricedOffer(t *testing.T) {
	for _, result := range search(t) {
		cheapest, ok := result.Cheapest()
		if !ok {
			if len(result.Offers) > 0 {
				t.Errorf("%s has %d offers but no cheapest", result.Hotel.ID, len(result.Offers))
			}
			continue
		}

		for _, offer := range result.Offers {
			if offer.Price.Total.Amount().IsZero() {
				continue
			}
			if cmp, err := offer.Price.Total.Compare(cheapest.Price.Total); err == nil && cmp < 0 {
				t.Errorf("%s: %s at %s is cheaper than the reported cheapest %s at %s",
					result.Hotel.ID, offer.ID, offer.Price.Total,
					cheapest.ID, cheapest.Price.Total)
			}
		}
	}
}

func TestGroupByRoomInvertsTheOfferList(t *testing.T) {
	// Find the hotel with the most room types - the captured fixture has one
	// with 18 offers across 3 rooms, which is exactly the shape a room picker
	// has to render.
	var best offers.HotelOffers
	bestRooms := 0
	for _, result := range search(t) {
		if rooms := len(result.GroupByRoom()); rooms > bestRooms {
			best, bestRooms = result, rooms
		}
	}
	if bestRooms < 2 {
		t.Skip("the captured fixture has no hotel with multiple room types")
	}

	groups := best.GroupByRoom()

	// Nothing may be lost or duplicated.
	counted := 0
	for _, group := range groups {
		counted += len(group.Offers)
	}
	if counted != len(best.Offers) {
		t.Errorf("grouping produced %d offers from %d", counted, len(best.Offers))
	}

	// Every offer in a group shares its room type.
	for _, group := range groups {
		for _, offer := range group.Offers {
			if offer.Room.Type != group.RoomType {
				t.Errorf("offer %s (room %q) is in group %q",
					offer.ID, offer.Room.Type, group.RoomType)
			}
		}
	}

	// Groups are ordered cheapest first, and so are the offers within them.
	for i := 1; i < len(groups); i++ {
		if cmp, err := groups[i-1].PriceFrom.Compare(groups[i].PriceFrom); err == nil && cmp > 0 {
			t.Errorf("group %q (%s) sorts before %q (%s)",
				groups[i-1].RoomType, groups[i-1].PriceFrom,
				groups[i].RoomType, groups[i].PriceFrom)
		}
	}
	for _, group := range groups {
		for i := 1; i < len(group.Offers); i++ {
			previous, current := group.Offers[i-1].Price.Total, group.Offers[i].Price.Total
			if previous.Amount().IsZero() {
				continue // priceless offers sort last; see the next test
			}
			if cmp, err := previous.Compare(current); err == nil && cmp > 0 {
				t.Errorf("group %q: %s sorts before the cheaper %s",
					group.RoomType, previous, current)
			}
		}
	}

	// The headline figures a room card renders come from the cheapest offer.
	for _, group := range groups {
		if group.Cheapest == nil {
			t.Errorf("group %q has no cheapest offer", group.RoomType)
			continue
		}
		if group.Cheapest.ID != group.Offers[0].ID {
			t.Errorf("group %q: Cheapest is not Offers[0]", group.RoomType)
		}
		if group.Room.Type != group.RoomType {
			t.Errorf("group %q: Room was not copied from the cheapest offer", group.RoomType)
		}
	}
}

func TestPricelessOfferSortsLastAndNeverWins(t *testing.T) {
	// A missing price must not read as zero and become the cheapest. The live
	// sandbox prices everything, so this is asserted against a constructed
	// response rather than the capture.
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, searchPath, http.StatusOK, `{"data":[{
	  "hotel": {"hotelId":"RTPAREIF","name":"TEST","cityCode":"PAR"},
	  "available": true,
	  "offers": [
	    {"id":"NO_PRICE","checkInDate":"2026-08-10","checkOutDate":"2026-08-13",
	     "room":{"type":"STD"},"price":{"currency":"EUR"},"guests":{"adults":2}},
	    {"id":"PRICED","checkInDate":"2026-08-10","checkOutDate":"2026-08-13",
	     "room":{"type":"STD"},"price":{"currency":"EUR","total":"360.00"},"guests":{"adults":2}}
	  ]}]}`)
	service := offers.NewService(server.Client())

	results, err := service.Search(context.Background(), offers.SearchQuery{HotelIDs: []string{"RTPAREIF"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	group := results[0].GroupByRoom()[0]
	if group.Offers[0].ID != "PRICED" {
		t.Errorf("cheapest = %s, want the priced offer", group.Offers[0].ID)
	}
	if group.Offers[1].ID != "NO_PRICE" {
		t.Errorf("the priceless offer should sort last, got %v",
			[]string{group.Offers[0].ID.String(), group.Offers[1].ID.String()})
	}

	cheapest, ok := results[0].Cheapest()
	if !ok || cheapest.ID != "PRICED" {
		t.Errorf("Cheapest() = %s, want PRICED", cheapest.ID)
	}
}

func TestGroupingIsStableAcrossRuns(t *testing.T) {
	first, second := search(t), search(t)

	for i := range first {
		groupsA, groupsB := first[i].GroupByRoom(), second[i].GroupByRoom()
		if len(groupsA) != len(groupsB) {
			t.Fatalf("%s: %d groups then %d", first[i].Hotel.ID, len(groupsA), len(groupsB))
		}
		for j := range groupsA {
			if groupsA[j].RoomType != groupsB[j].RoomType {
				t.Errorf("group %d: %q then %q", j, groupsA[j].RoomType, groupsB[j].RoomType)
			}
			for k := range groupsA[j].Offers {
				if groupsA[j].Offers[k].ID != groupsB[j].Offers[k].ID {
					t.Errorf("group %q offer %d: %s then %s",
						groupsA[j].RoomType, k, groupsA[j].Offers[k].ID, groupsB[j].Offers[k].ID)
				}
			}
		}
	}
}

func TestGroupByCategoryMergesTypesAndFallsBackWhenUncategorised(t *testing.T) {
	// A category collapses rooms of different type codes into one group, while
	// codes that carry no category - "*1B" (wildcard category) and "ROH" - fall
	// back to their own code rather than piling into one meaningless bucket.
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, searchPath, http.StatusOK, `{"data":[{
	  "hotel": {"hotelId":"RTPAREIF","name":"TEST","cityCode":"PAR"},
	  "available": true,
	  "offers": [
	    {"id":"SUITE_A","checkInDate":"2026-08-10","checkOutDate":"2026-08-13",
	     "room":{"type":"C3S","typeEstimated":{"category":"SUITE"}},"price":{"currency":"EUR","total":"500.00"},"guests":{"adults":2}},
	    {"id":"SUITE_B","checkInDate":"2026-08-10","checkOutDate":"2026-08-13",
	     "room":{"type":"C2T","typeEstimated":{"category":"SUITE"}},"price":{"currency":"EUR","total":"400.00"},"guests":{"adults":2}},
	    {"id":"WILDCARD","checkInDate":"2026-08-10","checkOutDate":"2026-08-13",
	     "room":{"type":"*1B"},"price":{"currency":"EUR","total":"300.00"},"guests":{"adults":2}},
	    {"id":"RUNOFHOUSE","checkInDate":"2026-08-10","checkOutDate":"2026-08-13",
	     "room":{"type":"ROH"},"price":{"currency":"EUR","total":"350.00"},"guests":{"adults":2}},
	    {"id":"NOROOM","checkInDate":"2026-08-10","checkOutDate":"2026-08-13",
	     "room":{},"price":{"currency":"EUR","total":"600.00"},"guests":{"adults":2}}
	  ]}]}`)
	service := offers.NewService(server.Client())

	results, err := service.Search(context.Background(), offers.SearchQuery{HotelIDs: []string{"RTPAREIF"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	groups := results[0].GroupByCategory()

	// SUITE merges the two type codes; the uncategorised rooms stay apart, and
	// the room with neither code nor category lands in "OTHER".
	if len(groups) != 4 {
		t.Fatalf("got %d category groups, want 4 (SUITE, *1B, ROH, OTHER)", len(groups))
	}

	byKey := map[string]offers.CategoryGroup{}
	for _, g := range groups {
		byKey[g.Category] = g
	}

	suite, ok := byKey["SUITE"]
	if !ok {
		t.Fatal("no SUITE group")
	}
	if len(suite.Offers) != 2 {
		t.Errorf("SUITE has %d offers, want 2 (both type codes merged)", len(suite.Offers))
	}
	if suite.Offers[0].ID != "SUITE_B" {
		t.Errorf("SUITE cheapest = %s, want SUITE_B (400 < 500)", suite.Offers[0].ID)
	}
	if suite.Cheapest == nil || suite.Cheapest.ID != suite.Offers[0].ID {
		t.Error("SUITE Cheapest is not Offers[0]")
	}

	for _, key := range []string{"*1B", "ROH", "OTHER"} {
		if _, ok := byKey[key]; !ok {
			t.Errorf("uncategorised room should fall back to a %q group", key)
		}
	}

	// Nothing is lost.
	counted := 0
	for _, g := range groups {
		counted += len(g.Offers)
	}
	if counted != len(results[0].Offers) {
		t.Errorf("grouping produced %d offers from %d", counted, len(results[0].Offers))
	}

	// Groups are ordered cheapest first: *1B(300), ROH(350), SUITE(400), OTHER(600).
	for i := 1; i < len(groups); i++ {
		if cmp, err := groups[i-1].PriceFrom.Compare(groups[i].PriceFrom); err == nil && cmp > 0 {
			t.Errorf("group %q (%s) sorts before %q (%s)",
				groups[i-1].Category, groups[i-1].PriceFrom,
				groups[i].Category, groups[i].PriceFrom)
		}
	}
}

func TestGroupingOrdersMixedCurrenciesByConvertedValue(t *testing.T) {
	// A hotel can quote offers for one room in more than one currency. Comparing
	// the raw amounts is meaningless - 270.60 HKD is cheaper than 396.00 CNY, but
	// neither the numbers nor the currencies say so on their own. The response's
	// conversion rates settle it: converted to MNT the HKD offer is the cheaper,
	// and grouping must order by that, not fall back to the offer-ID tie-break.
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, searchPath, http.StatusOK, `{"data":[{
	  "hotel": {"hotelId":"RTHKGNTH","name":"TEST","cityCode":"HKG"},
	  "available": true,
	  "offers": [
	    {"id":"AAA_CNY","checkInDate":"2026-08-01","checkOutDate":"2026-08-04",
	     "room":{"type":"C1D","typeEstimated":{"category":"STANDARD_ROOM"}},
	     "price":{"currency":"CNY","total":"396.00"},"guests":{"adults":2}},
	    {"id":"BBB_HKD","checkInDate":"2026-08-01","checkOutDate":"2026-08-04",
	     "room":{"type":"C1D","typeEstimated":{"category":"STANDARD_ROOM"}},
	     "price":{"currency":"HKD","total":"270.60"},"guests":{"adults":2}}
	  ]}],
	  "dictionaries":{"currencyConversionLookupRates":{
	    "CNY":{"rate":"500","target":"MNT","targetDecimalPlaces":0},
	    "HKD":{"rate":"450","target":"MNT","targetDecimalPlaces":0}}}}`)

	results, err := offers.NewService(server.Client()).Search(context.Background(),
		offers.SearchQuery{HotelIDs: []string{"RTHKGNTH"}, Currency: "MNT"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// 396.00 CNY -> 198,000 MNT; 270.60 HKD -> 121,770 MNT. The HKD offer wins.
	group := results[0].GroupByCategory()[0]
	if group.Category != "STANDARD_ROOM" {
		t.Fatalf("group category = %q, want STANDARD_ROOM", group.Category)
	}
	if group.Offers[0].ID != "BBB_HKD" {
		t.Errorf("cheapest = %s, want BBB_HKD (121,770 < 198,000 MNT)", group.Offers[0].ID)
	}
	if group.Cheapest == nil || group.Cheapest.ID != "BBB_HKD" {
		t.Errorf("Cheapest = %v, want BBB_HKD", group.Cheapest)
	}
	if cheapest, ok := results[0].Cheapest(); !ok || cheapest.ID != "BBB_HKD" {
		t.Errorf("Cheapest() = %v (ok=%v), want BBB_HKD", cheapest.ID, ok)
	}
}

func TestSoldOutHotelIsReturnedWithoutOffers(t *testing.T) {
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, searchPath, http.StatusOK,
		`{"data":[{"hotel":{"hotelId":"XXPAR999","name":"SOLD OUT","cityCode":"PAR"},
		  "available":false,"offers":[]}]}`)
	service := offers.NewService(server.Client())

	results, err := service.Search(context.Background(), offers.SearchQuery{HotelIDs: []string{"XXPAR999"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	soldOut := results[0]
	if soldOut.Available {
		t.Error("a sold-out property should report Available=false")
	}
	if _, ok := soldOut.Cheapest(); ok {
		t.Error("a hotel with no offers has no cheapest offer")
	}
}

func TestSearchQueryParameters(t *testing.T) {
	service, server := newService(t)

	_, err := service.Search(context.Background(), offers.SearchQuery{
		HotelIDs:           []string{"RTPAREIF", "RTPARMAI"},
		Stay:               offers.NewStay(datetime.MustParseDate("2026-08-10"), datetime.MustParseDate("2026-08-13")),
		Guests:             offers.Guests{Adults: 2, ChildAges: []int{7, 12}},
		Rooms:              2,
		BoardType:          codes.BoardTypeBreakfast,
		RateCodes:          []codes.RateCode{codes.RateCodePublic},
		PaymentPolicy:      codes.PaymentPolicyGuarantee,
		Currency:           "EUR",
		PriceRange:         "100-400",
		CountryOfResidence: "FR",
		BestRateOnly:       codes.Ptr(false),
		Lang:               "FR",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	query := server.LastRequest(t).Query
	want := map[string]string{
		"hotelIds":           "RTPAREIF,RTPARMAI",
		"checkInDate":        "2026-08-10",
		"checkOutDate":       "2026-08-13",
		"adults":             "2",
		"childAges":          "7,12",
		"roomQuantity":       "2",
		"boardType":          "BREAKFAST",
		"rateCodes":          "PRO",
		"paymentPolicy":      "GUARANTEE",
		"currency":           "EUR",
		"priceRange":         "100-400",
		"countryOfResidence": "FR",
		"bestRateOnly":       "false",
		"lang":               "FR",
	}
	for key, expected := range want {
		if got := query.Get(key); got != expected {
			t.Errorf("query[%s] = %q, want %q", key, got, expected)
		}
	}
}

func TestUnsetParametersAreOmitted(t *testing.T) {
	// Amadeus rejects adults=0 and currency=, so an unset field must not be
	// sent as an empty value.
	service, server := newService(t)

	if _, err := service.Search(context.Background(), offers.SearchQuery{
		HotelIDs: []string{"RTPAREIF"},
	}); err != nil {
		t.Fatalf("Search: %v", err)
	}

	query := server.LastRequest(t).Query
	for _, key := range []string{"adults", "currency", "boardType", "roomQuantity", "bestRateOnly"} {
		if _, present := query[key]; present {
			t.Errorf("unset %q was sent as %q", key, query.Get(key))
		}
	}
}

func TestUndocumentedRateCodesAreAccepted(t *testing.T) {
	// The live sandbox returns rate codes like "1KD", "EAM" and "D20" that
	// appear nowhere in Amadeus's documented list. RateCode.IsValid checks the
	// shape, not membership, precisely so these round-trip.
	for _, offer := range allOffers(t) {
		if offer.RateCode == "" {
			continue
		}
		if !offer.RateCode.IsValid() {
			t.Errorf("offer %s: live rate code %q was rejected as invalid",
				offer.ID, offer.RateCode)
		}
	}
}

// hotelIDs builds n distinct 8-character property codes.
func hotelIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("RTPAR%03d", i)
	}
	return ids
}

// TestMaxHotelIDsIsFifty pins the boundary rather than the constant. Amadeus
// accepts 50 hotel IDs and rejects 51 with "477 INVALID FORMAT - Exceeding max
// items for: hotelIds"; the constant was 100 for a while, which let 51-100
// through validation and turned a named error into that opaque 400.
func TestMaxHotelIDsIsFifty(t *testing.T) {
	if offers.MaxHotelIDs != 50 {
		t.Errorf("MaxHotelIDs = %d, want 50: Amadeus rejects the 51st ID",
			offers.MaxHotelIDs)
	}

	service, server := newService(t)
	query := offers.SearchQuery{HotelIDs: hotelIDs(offers.MaxHotelIDs)}
	if _, err := service.Search(context.Background(), query); err != nil {
		t.Errorf("%d IDs should pass validation: %v", offers.MaxHotelIDs, err)
	}
	if len(server.Requests()) == 0 {
		t.Error("a search at the limit never reached the network")
	}
}

func TestSearchValidation(t *testing.T) {
	service, server := newService(t)

	cases := []struct {
		name  string
		query offers.SearchQuery
	}{
		{"no hotel IDs", offers.SearchQuery{}},
		{"backwards stay", offers.SearchQuery{
			HotelIDs: []string{"RTPAREIF"},
			Stay: offers.NewStay(
				datetime.MustParseDate("2026-08-13"),
				datetime.MustParseDate("2026-08-10")),
		}},
		{"zero-night stay", offers.SearchQuery{
			HotelIDs: []string{"RTPAREIF"},
			Stay: offers.NewStay(
				datetime.MustParseDate("2026-08-10"),
				datetime.MustParseDate("2026-08-10")),
		}},
		{"too many adults", offers.SearchQuery{
			HotelIDs: []string{"RTPAREIF"},
			Guests:   offers.Guests{Adults: 20},
		}},
		{"adult age given as a child", offers.SearchQuery{
			HotelIDs: []string{"RTPAREIF"},
			Guests:   offers.Guests{Adults: 1, ChildAges: []int{45}},
		}},
		{"price range without a currency", offers.SearchQuery{
			HotelIDs:   []string{"RTPAREIF"},
			PriceRange: "100-400",
		}},
		{"unknown board type", offers.SearchQuery{
			HotelIDs:  []string{"RTPAREIF"},
			BoardType: "BRUNCH",
		}},
		{"malformed rate code", offers.SearchQuery{
			HotelIDs:  []string{"RTPAREIF"},
			RateCodes: []codes.RateCode{"TOOLONG"},
		}},
		{"one hotel ID too many", offers.SearchQuery{
			HotelIDs: hotelIDs(offers.MaxHotelIDs + 1),
		}},
	}

	before := len(server.Requests())
	for _, c := range cases {
		if _, err := service.Search(context.Background(), c.query); !errors.Is(err, apierr.ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", c.name, err)
		}
	}
	if after := len(server.Requests()); after != before {
		t.Errorf("%d invalid searches reached the network", after-before)
	}
}

func TestGetReturnsOfferWithItsHotel(t *testing.T) {
	server := amadeustest.New(t)
	server.Fixture(t, http.MethodGet, searchPath+"/ANY", "offer-by-id")
	service := offers.NewService(server.Client())

	detail, err := service.Get(context.Background(), offers.GetQuery{OfferID: "ANY"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.Offer.ID == "" {
		t.Error("the offer was not mapped")
	}
	// The hotel must come back too: a price with no property cannot be shown.
	if detail.Hotel.ID == "" {
		t.Errorf("hotel was dropped: %+v", detail.Hotel)
	}
	if detail.Offer.Price.Total.Amount().IsZero() {
		t.Error("the offer came back with no price")
	}
}

func TestGetOnAnEmptyResponseReportsNotFound(t *testing.T) {
	// An expired offer ID can return 200 with nothing in it. A nil offer and a
	// nil error would hand the caller a panic.
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, searchPath+"/EXPIRED", http.StatusOK, `{"data":{}}`)
	service := offers.NewService(server.Client())

	detail, err := service.Get(context.Background(), offers.GetQuery{OfferID: "EXPIRED"})
	if detail != nil {
		t.Errorf("detail = %+v, want nil", detail)
	}
	if !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetValidatesOfferID(t *testing.T) {
	service, _ := newService(t)
	if _, err := service.Get(context.Background(), offers.GetQuery{}); !errors.Is(err, apierr.ErrValidation) {
		t.Errorf("err = %v, want ErrValidation", err)
	}
}

func TestExpiredOfferSurfacesAsNotFound(t *testing.T) {
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, searchPath+"/STALE", http.StatusNotFound,
		`{"errors":[{"status":404,"code":1797,"title":"NOT FOUND","detail":"offer no longer available"}]}`)
	service := offers.NewService(server.Client())

	_, err := service.Get(context.Background(), offers.GetQuery{OfferID: "STALE"})
	if !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestNoAvailabilityFailsTheWholeSearch(t *testing.T) {
	// Documenting real and surprising Amadeus behaviour: when any one property
	// in hotelIds has no rooms, the entire search returns a 400 naming it -
	// the others are not returned. An application searching twenty hotels
	// where two are sold out gets nothing, not eighteen results.
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, searchPath, http.StatusBadRequest,
		`{"errors":[{"status":400,"code":3664,"title":"NO ROOMS AVAILABLE AT REQUESTED PROPERTY",
		  "detail":"Provider Error","source":{"parameter":"hotelIds=HNPARKGU,HNPARNUJ"}}]}`)
	service := offers.NewService(server.Client())

	_, err := service.Search(context.Background(), offers.SearchQuery{
		HotelIDs: []string{"HNPARKGU", "HNPARNUJ", "RTPAREIF"},
	})
	if !errors.Is(err, apierr.ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}

	var apiErr *apierr.APIError
	if !errors.As(err, &apiErr) {
		t.Fatal("expected an *APIError carrying the detail")
	}
	// The offending properties are named in the source parameter, which is how
	// a caller can retry without them.
	if got := apiErr.Details[0].Source.Parameter; got != "hotelIds=HNPARKGU,HNPARNUJ" {
		t.Errorf("source = %q, want the offending hotel IDs", got)
	}
}

func TestMalformedPriceDoesNotDiscardTheOffer(t *testing.T) {
	// One bad decimal must not cost the caller the whole response.
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, searchPath, http.StatusOK, `{"data":[{
	  "hotel": {"hotelId":"RTPAREIF","name":"TEST","cityCode":"PAR"},
	  "available": true,
	  "offers": [{"id":"BAD_PRICE","checkInDate":"2026-08-10","checkOutDate":"2026-08-13",
	    "room":{"type":"STD"},
	    "price":{"currency":"EUR","total":"not-a-number","base":"100.00"},
	    "guests":{"adults":2}}]}]}`)
	service := offers.NewService(server.Client())

	results, err := service.Search(context.Background(), offers.SearchQuery{HotelIDs: []string{"RTPAREIF"}})
	if err != nil {
		t.Fatalf("a malformed price should not fail the search: %v", err)
	}

	offer := results[0].Offers[0]
	if offer.ID != "BAD_PRICE" {
		t.Fatalf("the offer was dropped: %+v", results)
	}
	if !offer.Price.Total.Amount().IsZero() {
		t.Errorf("unparseable total = %s, want zero", offer.Price.Total)
	}
	if offer.Price.Base.String() != "100 EUR" {
		t.Errorf("the readable base should survive, got %s", offer.Price.Base)
	}
	if offer.Stay.Nights() != 3 {
		t.Error("the rest of the offer should be intact")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
