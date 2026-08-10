package inventory_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/apierr"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/geo"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeustest"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/inventory"
)

const (
	pathByCity    = "/v1/reference-data/locations/hotels/by-city"
	pathByGeocode = "/v1/reference-data/locations/hotels/by-geocode"
	pathByHotels  = "/v1/reference-data/locations/hotels/by-hotels"
)

// These run against hotels-by-city.json, captured from the live Amadeus sandbox
// by internal/capture. They assert on invariants rather than fixture positions,
// so a re-capture with different properties does not fail them spuriously.

func newService(t *testing.T) (inventory.Service, *amadeustest.Server) {
	t.Helper()
	server := amadeustest.New(t)
	server.Fixture(t, http.MethodGet, pathByCity, "hotels-by-city")
	server.Fixture(t, http.MethodGet, pathByGeocode, "hotels-by-city")
	server.Fixture(t, http.MethodGet, pathByHotels, "hotels-by-city")
	return inventory.NewService(server.Client()), server
}

func hotels(t *testing.T) []inventory.Hotel {
	t.Helper()
	service, _ := newService(t)

	found, err := service.ByCity(context.Background(), inventory.CityQuery{CityCode: "PAR"})
	if err != nil {
		t.Fatalf("ByCity: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("the fixture contains no hotels")
	}
	return found
}

// find returns the first hotel satisfying match, skipping when the captured
// data has no such case.
func find(t *testing.T, what string, match func(inventory.Hotel) bool) inventory.Hotel {
	t.Helper()
	for _, hotel := range hotels(t) {
		if match(hotel) {
			return hotel
		}
	}
	t.Skipf("no hotel in the captured fixture has %s", what)
	return inventory.Hotel{}
}

func TestEveryHotelIsUsablyMapped(t *testing.T) {
	for _, hotel := range hotels(t) {
		if hotel.ID == "" {
			t.Errorf("hotel has no ID: %+v", hotel)
			continue
		}
		if !hotel.ID.IsValid() {
			t.Errorf("%q is not a well-formed property code", hotel.ID)
		}
		if hotel.Name == "" {
			t.Errorf("%s has no name", hotel.ID)
		}
		if hotel.ChainCode == "" {
			t.Errorf("%s has no chain code", hotel.ID)
		}
	}
}

func TestNumericDupeIDBecomesAString(t *testing.T) {
	// Hotel List sends dupeId as a JSON number while Hotel Search sends the
	// same concept as a string. The domain normalises both.
	hotel := find(t, "a dupe ID", func(h inventory.Hotel) bool { return h.DupeID != "" })

	for _, r := range hotel.DupeID {
		if r < '0' || r > '9' {
			t.Errorf("%s: DupeID %q is not the number rendered as digits", hotel.ID, hotel.DupeID)
			break
		}
	}
}

func TestCoordinatesAreMapped(t *testing.T) {
	hotel := find(t, "coordinates", func(h inventory.Hotel) bool { return h.Position != nil })

	if err := hotel.Position.Validate(); err != nil {
		t.Errorf("%s has invalid coordinates: %v", hotel.ID, err)
	}
	if hotel.Position.Latitude == 0 && hotel.Position.Longitude == 0 {
		t.Errorf("%s mapped to 0,0", hotel.ID)
	}
}

func TestAbsentCoordinatesStayNilRatherThanBecomingNullIsland(t *testing.T) {
	// 0,0 is a real point in the Gulf of Guinea. An unlocatable property
	// defaulted to it would sit in the Atlantic and pass a non-zero check.
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, pathByCity, http.StatusOK,
		`{"data":[{"hotelId":"XXPAR999","chainCode":"XX","name":"UNLOCATED","iataCode":"PAR"}]}`)
	service := inventory.NewService(server.Client())

	found, err := service.ByCity(context.Background(), inventory.CityQuery{CityCode: "PAR"})
	if err != nil {
		t.Fatalf("ByCity: %v", err)
	}

	hotel := found[0]
	if hotel.Position != nil {
		t.Errorf("Position = %+v, want nil for a property with no geoCode", hotel.Position)
	}
	if hotel.Address != nil {
		t.Errorf("Address = %+v, want nil when Amadeus sent none", hotel.Address)
	}
	if hotel.DistanceFromSearch != nil {
		t.Errorf("DistanceFromSearch = %+v, want nil", hotel.DistanceFromSearch)
	}
	if hotel.DupeID != "" {
		t.Errorf("DupeID = %q, want empty when absent", hotel.DupeID)
	}
}

func TestSponsoredPlacementIsSurfaced(t *testing.T) {
	// Paid placement changes what result order means, so it must not be lost.
	sponsored := find(t, "sponsored placement", func(h inventory.Hotel) bool { return h.Sponsored })
	if !sponsored.Sponsored {
		t.Errorf("%s should be marked sponsored", sponsored.ID)
	}

	// And it must not be set on properties that did not pay for it.
	plain := 0
	for _, hotel := range hotels(t) {
		if !hotel.Sponsored {
			plain++
		}
	}
	if plain == 0 {
		t.Error("every hotel is marked sponsored, which is implausible")
	}
}

func TestDistanceFromSearchIsMapped(t *testing.T) {
	hotel := find(t, "a distance", func(h inventory.Hotel) bool { return h.DistanceFromSearch != nil })

	if hotel.DistanceFromSearch.Value <= 0 {
		t.Errorf("%s: distance %g", hotel.ID, hotel.DistanceFromSearch.Value)
	}
	if !hotel.DistanceFromSearch.Unit.IsValid() {
		t.Errorf("%s: unit %q is not a known distance unit", hotel.ID, hotel.DistanceFromSearch.Unit)
	}
	if hotel.DistanceFromSearch.Meters() <= 0 {
		t.Errorf("%s: Meters() = %g", hotel.ID, hotel.DistanceFromSearch.Meters())
	}
}

func TestPartialAddressIsPreserved(t *testing.T) {
	// Amadeus omits address parts rather than sending empty ones. A property
	// with a city but no postal code must keep the city.
	hotel := find(t, "an address", func(h inventory.Hotel) bool { return h.Address != nil })

	if hotel.Address.IsEmpty() {
		t.Errorf("%s: a mapped address should not be empty", hotel.ID)
	}
	if hotel.Address.CityName == "" && hotel.Address.CountryCode == "" && len(hotel.Address.Lines) == 0 {
		t.Errorf("%s: address carries nothing usable: %+v", hotel.ID, hotel.Address)
	}
}

func TestQueryParametersAreSentCorrectly(t *testing.T) {
	service, server := newService(t)

	_, err := service.ByCity(context.Background(), inventory.CityQuery{
		CityCode: "PAR",
		Filters: inventory.Filters{
			Radius:     10,
			RadiusUnit: geo.Miles,
			ChainCodes: []string{"MC", "HL"},
			Amenities:  []codes.Amenity{codes.AmenitySwimmingPool, codes.AmenitySpa},
			Ratings:    []codes.Rating{codes.Rating4, codes.Rating5},
			Source:     codes.HotelSourceAll,
		},
	})
	if err != nil {
		t.Fatalf("ByCity: %v", err)
	}

	query := server.LastRequest(t).Query
	want := map[string]string{
		"cityCode":    "PAR",
		"radius":      "10",
		"radiusUnit":  "MILE",
		"chainCodes":  "MC,HL",
		"amenities":   "SWIMMING_POOL,SPA",
		"ratings":     "4,5",
		"hotelSource": "ALL",
	}
	for key, expected := range want {
		if got := query.Get(key); got != expected {
			t.Errorf("query[%s] = %q, want %q", key, got, expected)
		}
	}
}

func TestUnsetFiltersAreOmittedEntirely(t *testing.T) {
	// Amadeus rejects radius=0 and radiusUnit=, so an unset filter must not be
	// sent as an empty value.
	service, server := newService(t)

	if _, err := service.ByCity(context.Background(), inventory.CityQuery{CityCode: "PAR"}); err != nil {
		t.Fatalf("ByCity: %v", err)
	}

	query := server.LastRequest(t).Query
	for _, key := range []string{"radius", "radiusUnit", "chainCodes", "amenities", "ratings", "hotelSource"} {
		if _, present := query[key]; present {
			t.Errorf("unset filter %q was sent as %q", key, query.Get(key))
		}
	}
}

func TestByGeocodeSendsCoordinates(t *testing.T) {
	service, server := newService(t)

	_, err := service.ByGeocode(context.Background(), inventory.GeocodeQuery{
		Position: geo.Coordinates{Latitude: 48.85, Longitude: 2.29},
	})
	if err != nil {
		t.Fatalf("ByGeocode: %v", err)
	}

	query := server.LastRequest(t).Query
	if query.Get("latitude") != "48.85" || query.Get("longitude") != "2.29" {
		t.Errorf("coordinates sent as %q, %q", query.Get("latitude"), query.Get("longitude"))
	}
}

func TestByIDsJoinsCodes(t *testing.T) {
	service, server := newService(t)

	_, err := service.ByIDs(context.Background(), inventory.IDsQuery{
		HotelIDs: []string{"MCLONGHM", "ACPAR419"},
	})
	if err != nil {
		t.Fatalf("ByIDs: %v", err)
	}
	if got := server.LastRequest(t).Query.Get("hotelIds"); got != "MCLONGHM,ACPAR419" {
		t.Errorf("hotelIds = %q", got)
	}
}

// Validation happens before any network call, so a caller learns which field is
// wrong instead of decoding an Amadeus 400.
func TestValidationRejectsBadQueriesWithoutCallingAmadeus(t *testing.T) {
	service, server := newService(t)

	cases := []struct {
		name string
		call func() error
	}{
		{"missing city code", func() error {
			_, err := service.ByCity(context.Background(), inventory.CityQuery{})
			return err
		}},
		{"city code of the wrong length", func() error {
			_, err := service.ByCity(context.Background(), inventory.CityQuery{CityCode: "PARIS"})
			return err
		}},
		{"unknown amenity", func() error {
			_, err := service.ByCity(context.Background(), inventory.CityQuery{
				CityCode: "PAR",
				Filters:  inventory.Filters{Amenities: []codes.Amenity{"BABY-SITTING"}},
			})
			return err
		}},
		{"too many ratings", func() error {
			_, err := service.ByCity(context.Background(), inventory.CityQuery{
				CityCode: "PAR",
				Filters: inventory.Filters{Ratings: []codes.Rating{
					codes.Rating1, codes.Rating2, codes.Rating3, codes.Rating4, codes.Rating5,
				}},
			})
			return err
		}},
		{"radius unit Amadeus rejects here", func() error {
			_, err := service.ByCity(context.Background(), inventory.CityQuery{
				CityCode: "PAR",
				Filters:  inventory.Filters{RadiusUnit: geo.Feet},
			})
			return err
		}},
		{"coordinates out of range", func() error {
			_, err := service.ByGeocode(context.Background(), inventory.GeocodeQuery{
				Position: geo.Coordinates{Latitude: 100, Longitude: 0},
			})
			return err
		}},
		{"no hotel IDs", func() error {
			_, err := service.ByIDs(context.Background(), inventory.IDsQuery{})
			return err
		}},
		{"malformed hotel ID", func() error {
			_, err := service.ByIDs(context.Background(), inventory.IDsQuery{HotelIDs: []string{"nope"}})
			return err
		}},
	}

	before := len(server.Requests())
	for _, c := range cases {
		if err := c.call(); !errors.Is(err, apierr.ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", c.name, err)
		}
	}
	if after := len(server.Requests()); after != before {
		t.Errorf("%d invalid queries reached the network", after-before)
	}
}

func TestValidationReportsEveryProblemAtOnce(t *testing.T) {
	service, _ := newService(t)

	_, err := service.ByCity(context.Background(), inventory.CityQuery{
		CityCode: "",
		Filters: inventory.Filters{
			Radius:     -1,
			Amenities:  []codes.Amenity{"NOT_A_THING"},
			ChainCodes: []string{"TOOLONG"},
		},
	})

	var errs apierr.ValidationErrors
	if !errors.As(err, &errs) {
		t.Fatalf("err = %v (%T), want ValidationErrors", err, err)
	}
	if len(errs) != 4 {
		t.Errorf("reported %d problems, want all 4: %v", len(errs), err)
	}
}

func TestAmadeusErrorsSurfaceTyped(t *testing.T) {
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, pathByCity, http.StatusBadRequest,
		`{"errors":[{"status":400,"code":572,"title":"INVALID OPTION","detail":"unknown city","source":{"parameter":"cityCode"}}]}`)
	service := inventory.NewService(server.Client())

	_, err := service.ByCity(context.Background(), inventory.CityQuery{CityCode: "ZZZ"})
	if !errors.Is(err, apierr.ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}

	var apiErr *apierr.APIError
	if !errors.As(err, &apiErr) {
		t.Fatal("error should also be an *APIError")
	}
	if len(apiErr.Details) != 1 || apiErr.Details[0].Code != 572 {
		t.Errorf("details = %+v", apiErr.Details)
	}
	if apiErr.Details[0].Source.Parameter != "cityCode" {
		t.Errorf("source = %+v, want the offending parameter", apiErr.Details[0].Source)
	}
}

func TestEmptyResultIsNotAnError(t *testing.T) {
	// A search matching nothing is a valid answer, not a failure.
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, pathByCity, http.StatusOK, `{"meta":{"count":0},"data":[]}`)
	service := inventory.NewService(server.Client())

	found, err := service.ByCity(context.Background(), inventory.CityQuery{CityCode: "PAR"})
	if err != nil {
		t.Fatalf("empty result returned an error: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("got %d hotels, want 0", len(found))
	}
}

func TestIDsHelperFeedsTheOtherContexts(t *testing.T) {
	found := hotels(t)
	ids := inventory.IDs(found)

	if len(ids) != len(found) {
		t.Fatalf("IDs returned %d for %d hotels", len(ids), len(found))
	}
	for i, id := range ids {
		if id != string(found[i].ID) {
			t.Errorf("IDs[%d] = %q, want %q", i, id, found[i].ID)
		}
	}
}

func TestContextCancellationPropagates(t *testing.T) {
	service, _ := newService(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.ByCity(ctx, inventory.CityQuery{CityCode: "PAR"}); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestStarRatingAndAmenitiesAreMapped(t *testing.T) {
	// Amadeus echoes the star rating back on every hotel when the search filters
	// on ratings, and the amenity codes when it filters on amenities. Dropping
	// them means a caller who filtered by stars cannot show the stars.
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, pathByCity, http.StatusOK, `{"data":[
	  {"hotelId":"BWPAR635","name":"BW PREMIER FAUBOURG 88","chainCode":"BW",
	   "iataCode":"PAR","rating":3,"amenities":["WIFI","SWIMMING_POOL"]},
	  {"hotelId":"HLPAR266","name":"HILTON PARIS OPERA","chainCode":"HL",
	   "iataCode":"PAR"}]}`)

	hotels, err := inventory.NewService(server.Client()).ByCity(
		context.Background(), inventory.CityQuery{CityCode: "PAR"})
	if err != nil {
		t.Fatalf("ByCity: %v", err)
	}
	if len(hotels) != 2 {
		t.Fatalf("got %d hotels", len(hotels))
	}

	if hotels[0].Rating != 3 {
		t.Errorf("Rating = %d, want 3", hotels[0].Rating)
	}
	if len(hotels[0].Amenities) != 2 || hotels[0].Amenities[0] != codes.AmenityWifi {
		t.Errorf("Amenities = %v", hotels[0].Amenities)
	}
	// A hotel Amadeus sent neither for must not invent them.
	if hotels[1].Rating != 0 || len(hotels[1].Amenities) != 0 {
		t.Errorf("absent fields were invented: rating=%d amenities=%v",
			hotels[1].Rating, hotels[1].Amenities)
	}
}
