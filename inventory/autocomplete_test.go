package inventory_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/apierr"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeustest"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/inventory"
)

const pathByKeyword = "/v1/reference-data/locations/hotel"

// These run against hotels-by-keyword.json. Like the Hotel List tests they
// assert on invariants rather than fixture positions, so a re-capture does not
// fail them spuriously.

func newKeywordService(t *testing.T) (inventory.Service, *amadeustest.Server) {
	t.Helper()
	server := amadeustest.New(t)
	server.Fixture(t, http.MethodGet, pathByKeyword, "hotels-by-keyword")
	return inventory.NewService(server.Client()), server
}

func suggestions(t *testing.T) []inventory.Suggestion {
	t.Helper()
	service, _ := newKeywordService(t)

	found, err := service.ByKeyword(context.Background(), inventory.KeywordQuery{Keyword: "PARI"})
	if err != nil {
		t.Fatalf("ByKeyword: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("the fixture contains no suggestions")
	}
	return found
}

func TestEverySuggestionIsUsablyMapped(t *testing.T) {
	for _, suggestion := range suggestions(t) {
		if suggestion.Name == "" {
			t.Errorf("suggestion has no name: %+v", suggestion)
		}
		if len(suggestion.HotelIDs) == 0 {
			t.Errorf("%q carries no property codes, so nothing can be done with it", suggestion.Name)
		}
		if !suggestion.SubType.IsValid() {
			t.Errorf("%q: sub type %q is not a known code", suggestion.Name, suggestion.SubType)
		}
		if suggestion.Relevance <= 0 {
			t.Errorf("%q: relevance %d", suggestion.Name, suggestion.Relevance)
		}
	}
}

func TestSuggestionIDsFeedTheOtherContexts(t *testing.T) {
	for _, suggestion := range suggestions(t) {
		ids := suggestion.IDs()
		if len(ids) != len(suggestion.HotelIDs) {
			t.Fatalf("IDs returned %d for %d codes", len(ids), len(suggestion.HotelIDs))
		}
		for i, id := range ids {
			if id != string(suggestion.HotelIDs[i]) {
				t.Errorf("IDs[%d] = %q, want %q", i, id, suggestion.HotelIDs[i])
			}
		}
	}
}

func TestKeywordSendsRepeatedSubTypesByDefault(t *testing.T) {
	// Amadeus requires at least one subType and expects the parameter repeated,
	// not comma-joined; an unset query must default to searching everything.
	service, server := newKeywordService(t)

	if _, err := service.ByKeyword(context.Background(), inventory.KeywordQuery{Keyword: "PARI"}); err != nil {
		t.Fatalf("ByKeyword: %v", err)
	}

	query := server.LastRequest(t).Query
	if got := query.Get("keyword"); got != "PARI" {
		t.Errorf("keyword = %q", got)
	}

	sent := query["subType"]
	if len(sent) != 2 {
		t.Fatalf("subType sent %d times, want once per value: %q", len(sent), sent)
	}
	for _, want := range []string{"HOTEL_LEISURE", "HOTEL_GDS"} {
		found := false
		for _, got := range sent {
			found = found || got == want
		}
		if !found {
			t.Errorf("subType %q was not sent; got %q", want, sent)
		}
	}
}

func TestKeywordOptionalParametersAreSent(t *testing.T) {
	service, server := newKeywordService(t)

	_, err := service.ByKeyword(context.Background(), inventory.KeywordQuery{
		Keyword:     "PARI",
		SubTypes:    []codes.HotelSubType{codes.HotelSubTypeGDS},
		CountryCode: "FR",
		Language:    "FR",
		Max:         5,
	})
	if err != nil {
		t.Fatalf("ByKeyword: %v", err)
	}

	query := server.LastRequest(t).Query
	if got := query["subType"]; len(got) != 1 || got[0] != "HOTEL_GDS" {
		t.Errorf("subType = %q, want the one requested", got)
	}
	want := map[string]string{"countryCode": "FR", "lang": "FR", "max": "5"}
	for key, expected := range want {
		if got := query.Get(key); got != expected {
			t.Errorf("query[%s] = %q, want %q", key, got, expected)
		}
	}
}

func TestKeywordUnsetOptionalsAreOmitted(t *testing.T) {
	service, server := newKeywordService(t)

	if _, err := service.ByKeyword(context.Background(), inventory.KeywordQuery{Keyword: "PARI"}); err != nil {
		t.Fatalf("ByKeyword: %v", err)
	}

	query := server.LastRequest(t).Query
	for _, key := range []string{"countryCode", "lang", "max"} {
		if _, present := query[key]; present {
			t.Errorf("unset parameter %q was sent as %q", key, query.Get(key))
		}
	}
}

func TestSuggestionAbsentBlocksStayNil(t *testing.T) {
	// Autocomplete marks address and geoCode optional, so a match Amadeus
	// cannot place must not gain a zero position or an empty address.
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, pathByKeyword, http.StatusOK,
		`{"data":[{"id":1,"type":"location","name":"UNPLACED HOTEL","iataCode":"PAR","subType":"HOTEL_GDS","hotelIds":["XXPAR999"]}]}`)
	service := inventory.NewService(server.Client())

	found, err := service.ByKeyword(context.Background(), inventory.KeywordQuery{Keyword: "UNPL"})
	if err != nil {
		t.Fatalf("ByKeyword: %v", err)
	}

	suggestion := found[0]
	if suggestion.Position != nil {
		t.Errorf("Position = %+v, want nil for a match with no geoCode", suggestion.Position)
	}
	if suggestion.Address != nil {
		t.Errorf("Address = %+v, want nil when Amadeus sent none", suggestion.Address)
	}
}

func TestKeywordValidationRejectsBadQueriesWithoutCallingAmadeus(t *testing.T) {
	service, server := newKeywordService(t)

	cases := []struct {
		name  string
		query inventory.KeywordQuery
	}{
		{"missing keyword", inventory.KeywordQuery{}},
		{"keyword below the four Amadeus requires", inventory.KeywordQuery{Keyword: "PAR"}},
		{"keyword beyond forty characters", inventory.KeywordQuery{
			Keyword: "AN IMPLAUSIBLY LONG HOTEL NAME KEYWORD OVER FORTY"}},
		{"unknown sub type", inventory.KeywordQuery{
			Keyword: "PARI", SubTypes: []codes.HotelSubType{"HOTEL_SPACE"}}},
		{"country code of the wrong length", inventory.KeywordQuery{Keyword: "PARI", CountryCode: "FRA"}},
		{"language of the wrong length", inventory.KeywordQuery{Keyword: "PARI", Language: "FRE"}},
		{"max beyond what Amadeus returns", inventory.KeywordQuery{Keyword: "PARI", Max: 21}},
		{"negative max", inventory.KeywordQuery{Keyword: "PARI", Max: -1}},
	}

	before := len(server.Requests())
	for _, c := range cases {
		if _, err := service.ByKeyword(context.Background(), c.query); !errors.Is(err, apierr.ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", c.name, err)
		}
	}
	if after := len(server.Requests()); after != before {
		t.Errorf("%d invalid queries reached the network", after-before)
	}
}

func TestEmptySuggestionsAreNotAnError(t *testing.T) {
	// A keyword matching nothing is the normal case while a user is mid-word.
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, pathByKeyword, http.StatusOK, `{"meta":{"count":0},"data":[]}`)
	service := inventory.NewService(server.Client())

	found, err := service.ByKeyword(context.Background(), inventory.KeywordQuery{Keyword: "ZZZZ"})
	if err != nil {
		t.Fatalf("empty result returned an error: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("got %d suggestions, want 0", len(found))
	}
}
