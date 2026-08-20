//go:build live

package livetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	sdk "github.com/techpartners-asia/amadeus-hotel-integration/v2"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/inventory"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/offers"
)

// This file investigates one specific disagreement: a search asking for four
// rooms for four adults that comes back carrying offers for five.
//
// roomQuantity is not cosmetic. It is what the price covers, so an offer
// quoting five rooms against a four-room request is either five rooms' worth of
// money or a mislabelled single room, and the two are impossible to tell apart
// from the mapped struct alone. The raw request and the raw response are the
// only evidence that settles it, which is what this test collects.
//
// The SDK's own Debug logging cannot supply it: shouldLogResponseBody omits the
// body of a successful search, precisely to keep hundred-kilobyte payloads out
// of a log. So the raw traffic is captured at the transport instead.

// defaultRooms and defaultAdults are the occupancy under investigation. Four
// adults in four rooms is one guest per room, which is the least ambiguous
// thing a hotel can be asked to price.
const (
	defaultRooms  = 4
	defaultAdults = 4
)

// requestedRooms and requestedAdults are the occupancy actually searched for.
//
// They are variables rather than constants so the occupancy can be varied from
// the command line without editing the test, which is what an investigation
// needs:
//
//	ROOMQTY_ROOMS=0 ROOMQTY_ADULTS=4 go test -tags live ./livetest/ -run RoomQuantity -v
//
// Rooms=0 is meaningful rather than invalid: SearchQuery omits roomQuantity
// entirely when Rooms is zero, which is how you find out what Amadeus returns
// when it was never asked.
var (
	requestedRooms  = envInt("ROOMQTY_ROOMS", defaultRooms)
	requestedAdults = envInt("ROOMQTY_ADULTS", defaultAdults)
)

// envInt reads an integer from the environment, falling back to fallback when
// it is unset or unreadable.
func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil {
		return fallback
	}
	return value
}

// exchange is one raw HTTP round trip, kept whole.
type exchange struct {
	Method string
	URL    string
	Header http.Header
	Status int
	Body   []byte
}

// recorder is an http.RoundTripper that keeps every exchange it carries.
//
// It reads the response body to record it and then hands an identical reader
// back to the SDK, so the client below it decodes exactly what was captured
// here rather than a second, differently-timed response.
type recorder struct {
	base http.RoundTripper

	mu        sync.Mutex
	exchanges []exchange
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := r.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	body, readErr := io.ReadAll(res.Body)
	res.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	res.Body = io.NopCloser(bytes.NewReader(body))

	// The token exchange is not traffic worth keeping, and its response
	// carries a bearer token that has no business in a test log.
	if !strings.Contains(req.URL.Path, "/security/oauth2/token") {
		r.mu.Lock()
		r.exchanges = append(r.exchanges, exchange{
			Method: req.Method,
			URL:    req.URL.String(),
			Header: redactHeader(req.Header),
			Status: res.StatusCode,
			Body:   body,
		})
		r.mu.Unlock()
	}

	return res, nil
}

// reset drops the recorded history, so a focused re-query can be dumped
// without the sweep that found it.
func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.exchanges = nil
}

// last returns the most recent exchange, and reports false when the call it
// was meant to capture never reached the network.
func (r *recorder) last() (exchange, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.exchanges) == 0 {
		return exchange{}, false
	}
	return r.exchanges[len(r.exchanges)-1], true
}

// redactHeader copies the request headers with the bearer token blanked. The
// SDK redacts its logs for the same reason; a test log is no safer a place for
// a credential than a production one.
func redactHeader(h http.Header) http.Header {
	out := h.Clone()
	if out == nil {
		out = http.Header{}
	}
	if out.Get("Authorization") != "" {
		out.Set("Authorization", "Bearer [REDACTED]")
	}
	return out
}

// dump writes the exchange out as it went over the wire.
//
// The body is printed as received rather than re-indented: the point of a raw
// capture is that it is the bytes Amadeus sent, and a pretty-printer is one
// more thing that could be lying to you. Pipe the output through jq if you want
// it formatted.
func (e exchange) dump(t *testing.T, label string) {
	t.Helper()

	var out strings.Builder
	fmt.Fprintf(&out, "\n======== RAW REQUEST (%s) ========\n%s %s\n", label, e.Method, e.URL)

	keys := make([]string, 0, len(e.Header))
	for key := range e.Header {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&out, "%s: %s\n", key, strings.Join(e.Header[key], ", "))
	}

	fmt.Fprintf(&out, "======== RAW RESPONSE (%s) status %d, %d bytes ========\n%s\n",
		label, e.Status, len(e.Body), e.Body)
	fmt.Fprintf(&out, "======== END (%s) ========", label)

	t.Log(out.String())
}

// newRecordingClient returns a client whose traffic is captured, skipping the
// test when no credentials are set.
func newRecordingClient(t *testing.T) (*sdk.Client, *recorder) {
	t.Helper()

	rec := &recorder{base: http.DefaultTransport}
	return newClientWithTransport(t, rec), rec
}

// mismatch is one offer whose roomQuantity disagrees with the request.
type mismatch struct {
	hotelID string
	offer   offers.Offer
}

// TestOffersRoomQuantityMismatch asks for four rooms for four adults and lists
// every offer that comes back covering some other number of rooms, with the
// raw request and raw response for each.
//
// Amadeus is asked twice on purpose. The sweep prices twenty properties at once
// because a mismatch is rare and one hotel is unlikely to show it; the re-query
// then prices the offending property alone, so the raw pair that gets printed
// is small enough to read and contains nothing but the case in question.
func TestOffersRoomQuantityMismatch(t *testing.T) {
	client, rec := newRecordingClient(t)
	ctx := testContext(t)

	cities := searchCities()
	stay := stayDates()
	t.Logf("pricing %v for %s to %s, %d adults in %d rooms",
		cities, stay.CheckIn, stay.CheckOut, requestedAdults, requestedRooms)

	var (
		mismatches []mismatch
		quantities = map[int]int{}
		priced     int
		firstOK    exchange
		haveFirst  bool
	)

	for _, city := range cities {
		ids := pricableHotelIDs(t, ctx, client, city)

		for _, batch := range batches(ids, 20) {
			results, err := client.Offers.Search(ctx, searchFor(batch, stay))
			captured, ok := rec.last()

			// A batch Amadeus refuses is expected here rather than
			// exceptional: the sandbox is full of properties with no inventory
			// and codes it no longer recognises, and one of either fails the
			// whole request. Dropping the properties it named and asking again
			// is what keeps the rest of the batch searched. It objects to one
			// property at a time, so this repeats - bounded, since each pass
			// must shrink the batch to continue.
			remaining := batch
			for attempt := 0; err != nil && attempt < maxDropAndRetry; attempt++ {
				retry := without(remaining, rejectedHotelIDs(err))
				if len(retry) == 0 || len(retry) == len(remaining) {
					break
				}
				t.Logf("%s: %v", city, err)
				t.Logf("%s: retrying %d of %d properties without the ones Amadeus rejected",
					city, len(retry), len(remaining))

				remaining = retry
				results, err = client.Offers.Search(ctx, searchFor(remaining, stay))
				captured, ok = rec.last()
			}
			if err != nil {
				t.Logf("%s: batch of %d refused: %v", city, len(batch), err)
				if ok {
					captured.dump(t, "refused batch in "+city)
				}
				continue
			}
			if ok && !haveFirst {
				firstOK, haveFirst = captured, true
			}

			for _, result := range results {
				for _, offer := range result.Offers {
					priced++
					quantities[offer.RoomQuantity]++

					// Zero is Amadeus declining to say, which conventionally
					// means one room and is a different complaint from this
					// one. Only a stated quantity that contradicts the request
					// counts - and when no quantity was requested there is
					// nothing for a response to contradict.
					if requestedRooms > 0 && offer.RoomQuantity > 0 && offer.RoomQuantity != requestedRooms {
						mismatches = append(mismatches, mismatch{
							hotelID: result.Hotel.ID,
							offer:   offer,
						})
					}
				}
			}
		}
	}

	if priced == 0 {
		if haveFirst {
			firstOK.dump(t, "no offers")
		}
		t.Skipf("no property in %v priced four rooms for four adults on these dates", cities)
	}
	t.Logf("priced %d offers; roomQuantity distribution: %s", priced, distribution(quantities))

	if len(mismatches) == 0 {
		// Nothing to report, but the raw pair for a search that behaved is
		// still the useful thing to look at afterwards.
		if haveFirst {
			firstOK.dump(t, "matching search")
		}
		t.Logf("every offer came back at roomQuantity %d or unstated", requestedRooms)
		return
	}

	t.Logf("%d offer(s) disagree with the requested roomQuantity of %d", len(mismatches), requestedRooms)
	for _, m := range mismatches {
		t.Errorf("hotel %s offer %s: requested roomQuantity %d, got %d (priced for %d adults, %s %s)",
			m.hotelID, m.offer.ID, requestedRooms, m.offer.RoomQuantity,
			m.offer.Guests.Adults, m.offer.Price.Total.Amount(), m.offer.Price.Currency)
	}

	// One focused re-query per offending property, so each printed pair shows
	// the case on its own rather than buried in twenty hotels of results.
	for _, hotelID := range distinctHotels(mismatches) {
		rec.reset()
		if _, err := client.Offers.Search(ctx, searchFor([]string{hotelID}, stay)); err != nil {
			t.Logf("re-querying %s: %v", hotelID, err)
		}
		captured, ok := rec.last()
		if !ok {
			t.Logf("re-querying %s produced no traffic to capture", hotelID)
			continue
		}
		captured.dump(t, "roomQuantity mismatch at "+hotelID)
	}
}

// searchFor builds the four-rooms-for-four-adults query.
//
// BestRateOnly is off deliberately. With Amadeus's default every property
// collapses to its cheapest rate, and a mismatched roomQuantity on any other
// rate would never be seen.
func searchFor(hotelIDs []string, stay offers.Stay) offers.SearchQuery {
	return offers.SearchQuery{
		HotelIDs:     hotelIDs,
		Stay:         stay,
		Guests:       offers.Guests{Adults: requestedAdults},
		Rooms:        requestedRooms,
		BestRateOnly: codes.Ptr(false),
	}
}

// searchCities returns the cities to sweep.
//
// One city is enough to see the shape of a response but not to find a rare
// disagreement, so the list is configurable: a mismatch that never appears in
// Paris may be routine for whichever provider serves another market.
//
//	ROOMQTY_CITIES=PAR,LON,NYC go test -tags live ./livetest/ -run RoomQuantity -v
func searchCities() []string {
	raw := os.Getenv("ROOMQTY_CITIES")
	if strings.TrimSpace(raw) == "" {
		return []string{"PAR"}
	}

	var cities []string
	for _, city := range strings.Split(raw, ",") {
		if city = strings.ToUpper(strings.TrimSpace(city)); city != "" {
			cities = append(cities, city)
		}
	}
	return cities
}

const (
	// maxHotelsPerCity bounds the sweep: enough properties per city to find a
	// rare offer without turning one test run into a hundred calls.
	maxHotelsPerCity = 100
	// maxDropAndRetry bounds how many times a refused batch is narrowed and
	// re-sent. Amadeus objects to one property per response, so a batch with
	// many dead codes would otherwise be re-sent once per code.
	maxDropAndRetry = 6
)

// pricableHotelIDs returns properties worth pricing in one city.
//
// It asks for the chain the sandbox actually holds inventory for first, since
// most sandbox properties are stubs that price nothing however the search is
// widened, and falls back to the direct-chain source when that chain returns
// nothing - which is what happens against production, where any chain prices.
func pricableHotelIDs(t *testing.T, ctx context.Context, client *sdk.Client, city string) []string {
	t.Helper()

	// bookableChain matches the constant internal/capture uses for the same
	// reason; the two are independent because a test may not import internal
	// tooling.
	const bookableChain = "RT"

	// The chain query goes first because its properties are the ones the
	// sandbox prices, but it is not the only source: in some cities it returns
	// a single property, and stopping there would leave the city effectively
	// unsearched. So every source contributes, in order of how likely it is to
	// price, deduplicated.
	queries := []inventory.CityQuery{
		{CityCode: city, Filters: inventory.Filters{ChainCodes: []string{bookableChain}}},
		{CityCode: city, Filters: inventory.Filters{Source: codes.HotelSourceDirectChain}},
		{CityCode: city},
	}

	seen := map[string]bool{}
	var ids []string
	for _, query := range queries {
		hotels, err := client.Inventory.ByCity(ctx, query)
		if err != nil {
			t.Logf("%s hotel list %+v: %v", city, query.Filters, err)
			continue
		}
		for _, id := range inventory.IDs(hotels) {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		if len(ids) >= maxHotelsPerCity {
			return ids[:maxHotelsPerCity]
		}
	}

	if len(ids) == 0 {
		t.Logf("no properties to price in %s", city)
	}
	return ids
}

// rejectedHotelIDs pulls the property codes Amadeus blamed out of an error.
//
// One bad code fails the whole batch - a request naming twenty hotels of which
// one is unknown returns a 400 and prices none of them - so the nineteen
// innocent properties are only reachable by dropping the named one and asking
// again. Amadeus reports them as source.parameter "hotelIds=A,B,C".
func rejectedHotelIDs(err error) []string {
	var apiErr *sdk.APIError
	if !errors.As(err, &apiErr) {
		return nil
	}

	var rejected []string
	for _, detail := range apiErr.Details {
		parameter, ok := strings.CutPrefix(detail.Source.Parameter, "hotelIds=")
		if !ok {
			continue
		}
		for _, id := range strings.Split(parameter, ",") {
			if id = strings.TrimSpace(id); id != "" {
				rejected = append(rejected, id)
			}
		}
	}
	return rejected
}

// without returns ids with every member of drop removed.
func without(ids, drop []string) []string {
	dropped := make(map[string]bool, len(drop))
	for _, id := range drop {
		dropped[id] = true
	}

	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !dropped[id] {
			out = append(out, id)
		}
	}
	return out
}

// batches splits ids into chunks of at most size, since Hotel Search takes at
// most offers.MaxHotelIDs property codes per request.
func batches(ids []string, size int) [][]string {
	var out [][]string
	for start := 0; start < len(ids); start += size {
		end := min(start+size, len(ids))
		out = append(out, ids[start:end])
	}
	return out
}

// distinctHotels lists each offending property once, in the order first seen,
// so a hotel with six mismatched rates is re-queried once rather than six
// times.
func distinctHotels(mismatches []mismatch) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mismatches {
		if !seen[m.hotelID] {
			seen[m.hotelID] = true
			out = append(out, m.hotelID)
		}
	}
	return out
}

// distribution renders the roomQuantity histogram, e.g. "1x4, 3x5".
func distribution(quantities map[int]int) string {
	keys := make([]int, 0, len(quantities))
	for quantity := range quantities {
		keys = append(keys, quantity)
	}
	sort.Ints(keys)

	parts := make([]string, 0, len(keys))
	for _, quantity := range keys {
		label := fmt.Sprintf("roomQuantity %d", quantity)
		if quantity == 0 {
			label = "roomQuantity unstated"
		}
		parts = append(parts, fmt.Sprintf("%s: %d offer(s)", label, quantities[quantity]))
	}
	return strings.Join(parts, ", ")
}

// TestOfferByIDKeepsRoomQuantity follows one offer from the search that priced
// it to the by-ID call that re-verifies it, and reports where the two disagree
// about how many rooms it covers.
//
// This is the path a booking actually takes. An application searches, shows the
// price, and re-fetches the offer by ID immediately before reserving it,
// because offer IDs expire. If roomQuantity survives the first call and not the
// second, then every caller reading it off the re-verified offer sees a
// different number from the one they searched for - which is a defect in the
// SDK's account of the offer even though both responses are faithfully mapped.
func TestOfferByIDKeepsRoomQuantity(t *testing.T) {
	client, rec := newRecordingClient(t)
	ctx := testContext(t)

	stay := stayDates()

	var (
		fromSearch offers.Offer
		hotelID    string
		found      bool
	)

	for _, city := range searchCities() {
		for _, batch := range batches(pricableHotelIDs(t, ctx, client, city), 20) {
			results, err := client.Offers.Search(ctx, searchFor(batch, stay))
			for attempt := 0; err != nil && attempt < maxDropAndRetry; attempt++ {
				retry := without(batch, rejectedHotelIDs(err))
				if len(retry) == 0 || len(retry) == len(batch) {
					break
				}
				batch = retry
				results, err = client.Offers.Search(ctx, searchFor(batch, stay))
			}
			if err != nil {
				continue
			}

			for _, result := range results {
				if len(result.Offers) > 0 {
					fromSearch, hotelID, found = result.Offers[0], result.Hotel.ID, true
					break
				}
			}
			if found {
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		t.Skip("no offer to re-verify")
	}

	searchExchange, _ := rec.last()
	t.Logf("search: hotel %s offer %s came back at roomQuantity %d for %d adults",
		hotelID, fromSearch.ID, fromSearch.RoomQuantity, fromSearch.Guests.Adults)

	// The by-ID call on its own, so its raw pair is not mixed in with the
	// twenty-hotel search that found the offer.
	rec.reset()
	detail, err := client.Offers.Get(ctx, offers.GetQuery{OfferID: fromSearch.ID})
	getExchange, captured := rec.last()
	if err != nil {
		if captured {
			getExchange.dump(t, "by-ID re-verify failed")
		}
		if errors.Is(err, sdk.ErrNotFound) {
			t.Skipf("offer %s expired before it could be re-verified", fromSearch.ID)
		}
		t.Fatalf("Offers.Get: %v", err)
	}

	t.Logf("by ID: offer %s came back at roomQuantity %d for %d adults",
		detail.Offer.ID, detail.Offer.RoomQuantity, detail.Offer.Guests.Adults)

	if detail.Offer.RoomQuantity == fromSearch.RoomQuantity {
		t.Logf("both calls agree at roomQuantity %d", fromSearch.RoomQuantity)
		return
	}

	// The two calls describe the same offer differently. Both raw pairs go in
	// the log, because the disagreement is only demonstrable by showing both.
	t.Errorf("offer %s: search said roomQuantity %d, by-ID said %d (requested %d)",
		fromSearch.ID, fromSearch.RoomQuantity, detail.Offer.RoomQuantity, requestedRooms)
	searchExchange.dump(t, "search that priced "+string(fromSearch.ID))
	getExchange.dump(t, "by-ID re-verify of "+string(fromSearch.ID))
}
