// Command capture records live Amadeus responses into the testdata fixtures
// the offline mapper tests run against.
//
// Run it when Amadeus changes a schema, or to replace hand-written fixtures
// with real payloads:
//
//	AMADEUS_CLIENT_ID=... AMADEUS_CLIENT_SECRET=... go run ./internal/capture
//
// It writes into each context's testdata directory. Re-running the mapper tests
// afterwards is the point of the exercise: a test that now fails is a field
// Amadeus changed, and a diff in the fixture shows exactly what.
//
// It never captures booking responses. Creating an order on the sandbox is
// still a booking, and this tool must not make one as a side effect of running
// it; booking/testdata/order.json is maintained by hand.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/apierr"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeus"
)

func main() {
	var (
		cityCode = flag.String("city", "PAR", "IATA city code to capture against")
		host     = flag.String("host", "https://test.travel.api.amadeus.com", "Amadeus host root")
		outDir   = flag.String("out", ".", "repository root to write testdata into")
	)
	flag.Parse()

	id, secret := os.Getenv("AMADEUS_CLIENT_ID"), os.Getenv("AMADEUS_CLIENT_SECRET")
	if id == "" || secret == "" {
		fmt.Fprintln(os.Stderr, "set AMADEUS_CLIENT_ID and AMADEUS_CLIENT_SECRET")
		os.Exit(1)
	}

	client := amadeus.NewClient(amadeus.Options{
		ClientID:     id,
		ClientSecret: secret,
		Host:         *host,
		Timeout:      90 * time.Second,
		UserAgent:    "amadeus-hotel-integration-capture",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := run(ctx, client, *cityCode, *outDir); err != nil {
		fmt.Fprintf(os.Stderr, "capture failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, client *amadeus.Client, cityCode, outDir string) error {
	// Hotel list first: everything downstream needs real property codes, and
	// hard-coding them would make this tool go stale.
	hotels, err := capture(ctx, client, outDir, "inventory", "hotels-by-city", amadeus.Request{
		Path:  "/v1/reference-data/locations/hotels/by-city",
		Query: url.Values{"cityCode": {cityCode}, "radius": {"5"}, "radiusUnit": {"KM"}},
	}, trimHotels(maxFixtureHotels))
	if err != nil {
		return fmt.Errorf("hotel list: %w", err)
	}

	ids, err := hotelIDs(hotels, 15)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("no hotels returned for %s; try another city", cityCode)
	}
	fmt.Printf("captured %d hotels for %s\n", len(ids), cityCode)

	// Hotel Name Autocomplete needs a keyword of four characters or more, so
	// the city flag cannot feed it; "PARI" matches plenty everywhere. The
	// capture warns rather than fails: the endpoint is documented on the
	// self-service catalog, and an Enterprise subscription may not carry it.
	if _, err := capture(ctx, client, outDir, "inventory", "hotels-by-keyword", amadeus.Request{
		Path:  "/v1/reference-data/locations/hotel",
		Query: url.Values{"keyword": {"PARI"}, "subType": {"HOTEL_LEISURE", "HOTEL_GDS"}},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: no autocomplete fixture captured: %v\n", err)
	}

	// Content, for the first property that has any.
	if _, err := capture(ctx, client, outDir, "content", "hotel", amadeus.Request{
		Path:  "/v3/reference-data/locations/by-hotel",
		Query: url.Values{"hotelID": {ids[0]}, "view": {"FULL"}},
	}); err != nil {
		return fmt.Errorf("hotel content: %w", err)
	}

	// Offers need properties that actually have inventory, which most of the
	// sandbox does not. Search the bookable chain specifically rather than
	// reusing the nearest hotels above - those are mostly test stubs with no
	// rooms, and pricing them returns nothing.
	bookable, err := bookableHotels(ctx, client, cityCode)
	if err != nil {
		return fmt.Errorf("finding bookable hotels: %w", err)
	}
	fmt.Printf("found %d properties on the bookable chain %q\n", len(bookable), bookableChain)

	// bestRateOnly=false is what makes the fixture useful: with the default,
	// every hotel collapses to one offer and the grouping tests have nothing
	// to group.
	checkIn := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	checkOut := time.Now().AddDate(0, 1, 3).Format("2006-01-02")

	search, err := captureOffers(ctx, client, outDir, bookable, checkIn, checkOut)
	if err != nil {
		return fmt.Errorf("hotel offers: %w", err)
	}

	// The same search asking for a currency the hotels do not quote in, which
	// is what makes Amadeus emit the dictionaries block. It does not convert
	// the prices; it supplies the rate and expects the caller to apply it, so
	// without this fixture there is no coverage of the conversion path.
	if _, err := captureConverted(ctx, client, outDir, bookable, checkIn, checkOut, conversionCurrency); err != nil {
		fmt.Fprintf(os.Stderr, "warning: no converted-currency fixture captured: %v\n", err)
	}

	offerID, err := firstOfferID(search)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: no bookable offer found, skipping offer-by-id: %v\n", err)
		return nil
	}

	if _, err := capture(ctx, client, outDir, "offers", "offer-by-id", amadeus.Request{
		Path: "/v3/shopping/hotel-offers/" + offerID,
	}); err != nil {
		return fmt.Errorf("offer by id: %w", err)
	}

	fmt.Println("\nRe-run the tests: a failure now is a field Amadeus changed.")
	fmt.Println("\tgo test ./...")
	return nil
}

// bookableChain is a chain whose properties carry bookable inventory in the
// Amadeus sandbox.
//
// Most sandbox properties are stubs with no rooms at all, so a search over the
// nearest hotels to a city centre returns no offers however far you widen it.
// This is sandbox-specific: against production, any chain will price.
const bookableChain = "RT"

// bookableHotels finds properties likely to have inventory, by filtering the
// hotel list to the chain the sandbox actually prices.
func bookableHotels(ctx context.Context, client *amadeus.Client, cityCode string) ([]string, error) {
	body, err := amadeus.DoRaw(ctx, client, amadeus.Request{
		Path: "/v1/reference-data/locations/hotels/by-city",
		Query: url.Values{
			"cityCode":   {cityCode},
			"chainCodes": {bookableChain},
		},
	})
	if err != nil {
		return nil, err
	}

	ids, err := hotelIDs(body, 20)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no %s properties in %s", bookableChain, cityCode)
	}
	return ids, nil
}

// conversionCurrency is the currency the converted-rates fixture asks for.
//
// MNT is deliberate: the Mongolian tögrög has no minor unit
// (targetDecimalPlaces 0), so the fixture exercises rounding to whole units.
// A target with two decimal places would let a rounding bug pass unnoticed.
const conversionCurrency = "MNT"

// captureConverted records a search in a foreign currency, which is the only
// way to get Amadeus to emit currencyConversionLookupRates.
//
// It keeps a couple of offers rather than the whole result: the point of this
// fixture is the dictionaries block, not breadth of pricing.
func captureConverted(ctx context.Context, client *amadeus.Client, outDir string, ids []string, checkIn, checkOut, currency string) ([]byte, error) {
	for _, id := range ids {
		body, err := capture(ctx, client, outDir, "offers", "search-converted", amadeus.Request{
			Path: "/v3/shopping/hotel-offers",
			Query: url.Values{
				"hotelIds":     {id},
				"checkInDate":  {checkIn},
				"checkOutDate": {checkOut},
				"adults":       {"2"},
				"currency":     {currency},
			},
		}, trimOffers(2))
		if err != nil {
			continue // this property has no availability; try the next
		}
		if !hasConversionRates(body) {
			// The hotel already quotes in the requested currency, so Amadeus
			// had no conversion to report. Keep looking.
			continue
		}
		fmt.Printf("captured %s conversion rates from %s\n", currency, id)
		return body, nil
	}
	return nil, fmt.Errorf("no property returned conversion rates for %s", currency)
}

// hasConversionRates reports whether a response carries the dictionaries block.
func hasConversionRates(body []byte) bool {
	var envelope struct {
		Dictionaries struct {
			Rates map[string]any `json:"currencyConversionLookupRates"`
		} `json:"dictionaries"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return false
	}
	return len(envelope.Dictionaries.Rates) > 0
}

// trimOffers caps the offers kept on the first hotel of a response.
func trimOffers(limit int) transform {
	return func(body []byte) ([]byte, error) {
		var envelope map[string]any
		if err := json.Unmarshal(body, &envelope); err != nil {
			return body, nil
		}

		data, ok := envelope["data"].([]any)
		if !ok || len(data) == 0 {
			return body, nil
		}
		hotel, ok := data[0].(map[string]any)
		if !ok {
			return body, nil
		}
		if offers, ok := hotel["offers"].([]any); ok && len(offers) > limit {
			hotel["offers"] = offers[:limit]
		}
		envelope["data"] = data[:1]
		return json.Marshal(envelope)
	}
}

// captureOffers searches for offers, narrowing the hotel set until it finds one
// Amadeus will price.
//
// Hotel Search fails the *entire* request with a 400 when any single property in
// hotelIds has no availability ("NO ROOMS AVAILABLE AT REQUESTED PROPERTY"),
// naming the offending codes. It does not simply omit them. So the strategy is
// to drop whatever it complains about and retry, and failing that fall back to
// one hotel at a time.
//
// This is worth knowing beyond this tool: an application searching twenty
// hotels where two are sold out gets nothing back, not eighteen results.
func captureOffers(ctx context.Context, client *amadeus.Client, outDir string, ids []string, checkIn, checkOut string) ([]byte, error) {
	search := func(set []string) ([]byte, error) {
		return capture(ctx, client, outDir, "offers", "search", amadeus.Request{
			Path: "/v3/shopping/hotel-offers",
			Query: url.Values{
				"hotelIds":     {strings.Join(set, ",")},
				"checkInDate":  {checkIn},
				"checkOutDate": {checkOut},
				"adults":       {"2"},
				"bestRateOnly": {"false"},
			},
		})
	}

	remaining := slices.Clone(ids)
	for attempt := 0; attempt < 4 && len(remaining) > 0; attempt++ {
		body, err := search(remaining)
		if err == nil {
			return body, nil
		}

		unavailable := unavailableHotels(err)
		if len(unavailable) == 0 {
			break
		}
		fmt.Printf("  %d of %d properties have no availability; retrying without them\n",
			len(unavailable), len(remaining))
		remaining = slices.DeleteFunc(remaining, func(id string) bool {
			return slices.Contains(unavailable, id)
		})
	}

	// Amadeus stopped naming the culprits, or removing them was not enough.
	// Fall back to one property at a time, which cannot be poisoned by another.
	fmt.Println("  falling back to one property at a time")
	for _, id := range ids {
		if body, err := search([]string{id}); err == nil {
			fmt.Printf("  captured offers for %s\n", id)
			return body, nil
		}
	}

	return nil, fmt.Errorf("no property in the captured set has availability for %s to %s; "+
		"try -city with somewhere busier, or different dates", checkIn, checkOut)
}

// unavailableHotels pulls the property codes out of a NO ROOMS AVAILABLE error.
// Amadeus reports them in the error's source parameter, as
// "hotelIds=HNPARKGU,HNPARNUJ".
func unavailableHotels(err error) []string {
	var apiErr *apierr.APIError
	if !errors.As(err, &apiErr) {
		return nil
	}

	var ids []string
	for _, detail := range apiErr.Details {
		parameter := detail.Source.Parameter
		_, list, found := strings.Cut(parameter, "hotelIds=")
		if !found {
			continue
		}
		for _, id := range strings.Split(list, ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// maxFixtureHotels caps how many properties the inventory fixture keeps.
//
// A city search returns everything: Paris alone came back with 3124 properties
// and 1.7 MB of JSON. That is a fine API response and a terrible test fixture -
// it bloats the repository and slows every test run - so the list is trimmed
// while the shape of each record is preserved exactly.
const maxFixtureHotels = 25

// transform optionally rewrites a captured body before it is written.
type transform func([]byte) ([]byte, error)

// trimHotels returns a transform that keeps at most limit entries of the data
// array, leaving every retained record untouched.
func trimHotels(limit int) transform {
	return func(body []byte) ([]byte, error) {
		var envelope map[string]any
		if err := json.Unmarshal(body, &envelope); err != nil {
			return body, nil
		}

		data, ok := envelope["data"].([]any)
		if !ok || len(data) <= limit {
			return body, nil
		}

		fmt.Printf("  trimming %d records to %d for a usable fixture\n", len(data), limit)
		envelope["data"] = data[:limit]
		if meta, ok := envelope["meta"].(map[string]any); ok {
			meta["count"] = limit
		}
		return json.Marshal(envelope)
	}
}

// capture performs one request and writes its indented body to
// <outDir>/<context>/testdata/<name>.json.
//
// It returns the untrimmed body, so callers still see every property the search
// found even when only a subset is written to disk.
func capture(ctx context.Context, client *amadeus.Client, outDir, contextDir, name string, req amadeus.Request, transforms ...transform) ([]byte, error) {
	body, err := amadeus.DoRaw(ctx, client, req)
	if err != nil {
		return nil, err
	}

	toWrite := body
	for _, apply := range transforms {
		if toWrite, err = apply(toWrite); err != nil {
			return nil, err
		}
	}

	var indented any
	if err := json.Unmarshal(toWrite, &indented); err != nil {
		return nil, fmt.Errorf("response was not JSON: %w", err)
	}
	pretty, err := json.MarshalIndent(indented, "", "  ")
	if err != nil {
		return nil, err
	}

	path := filepath.Join(outDir, contextDir, "testdata", name+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, append(pretty, '\n'), 0o644); err != nil {
		return nil, err
	}

	fmt.Printf("wrote %s (%d bytes)\n", path, len(pretty))
	return body, nil
}

// hotelIDs pulls up to limit property codes out of a hotel-list response.
func hotelIDs(body []byte, limit int) ([]string, error) {
	var envelope struct {
		Data []struct {
			HotelID string `json:"hotelId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}

	ids := make([]string, 0, limit)
	for _, hotel := range envelope.Data {
		if hotel.HotelID == "" {
			continue
		}
		if ids = append(ids, hotel.HotelID); len(ids) == limit {
			break
		}
	}
	return ids, nil
}

// firstOfferID finds an offer to re-fetch by ID.
func firstOfferID(body []byte) (string, error) {
	var envelope struct {
		Data []struct {
			Offers []struct {
				ID string `json:"id"`
			} `json:"offers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", err
	}

	for _, hotel := range envelope.Data {
		for _, offer := range hotel.Offers {
			if offer.ID != "" {
				return offer.ID, nil
			}
		}
	}
	return "", fmt.Errorf("the search returned no offers")
}
