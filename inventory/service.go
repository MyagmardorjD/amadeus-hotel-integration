package inventory

import (
	"context"
	"net/url"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeus"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeus/dto"
)

// Endpoint paths on the Hotel List API (v1.2), and the sibling Hotel Name
// Autocomplete API (v1) that ByKeyword wraps.
const (
	pathByCity    = "/v1/reference-data/locations/hotels/by-city"
	pathByGeocode = "/v1/reference-data/locations/hotels/by-geocode"
	pathByHotels  = "/v1/reference-data/locations/hotels/by-hotels"
	pathByKeyword = "/v1/reference-data/locations/hotel"
)

// Service finds hotels. Obtain one from the SDK client rather than building it
// directly:
//
//	client, _ := sdk.New(cfg)
//	hotels, err := client.Inventory.ByCity(ctx, inventory.CityQuery{CityCode: "PAR"})
//
// It is an interface so callers can substitute a fake in their own tests
// without an HTTP server.
type Service interface {
	// ByCity finds hotels around a city or airport code.
	ByCity(ctx context.Context, query CityQuery) ([]Hotel, error)
	// ByGeocode finds hotels around a geographic point.
	ByGeocode(ctx context.Context, query GeocodeQuery) ([]Hotel, error)
	// ByIDs looks up specific properties by their Amadeus codes.
	ByIDs(ctx context.Context, query IDsQuery) ([]Hotel, error)
	// ByKeyword suggests hotels whose names match a partial keyword, for
	// autocomplete on a search input.
	ByKeyword(ctx context.Context, query KeywordQuery) ([]Suggestion, error)
}

type service struct {
	client *amadeus.Client
}

// NewService returns the inventory service backed by client.
func NewService(client *amadeus.Client) Service {
	return &service{client: client}
}

func (s *service) ByCity(ctx context.Context, query CityQuery) ([]Hotel, error) {
	if err := query.validate(); err != nil {
		return nil, err
	}
	return s.fetch(ctx, pathByCity, query.params())
}

func (s *service) ByGeocode(ctx context.Context, query GeocodeQuery) ([]Hotel, error) {
	if err := query.validate(); err != nil {
		return nil, err
	}
	return s.fetch(ctx, pathByGeocode, query.params())
}

func (s *service) ByIDs(ctx context.Context, query IDsQuery) ([]Hotel, error) {
	if err := query.validate(); err != nil {
		return nil, err
	}
	return s.fetch(ctx, pathByHotels, query.params())
}

// ByKeyword does not share fetch: Hotel Name Autocomplete answers with
// location matches rather than the Hotel List record shape.
func (s *service) ByKeyword(ctx context.Context, query KeywordQuery) ([]Suggestion, error) {
	if err := query.validate(); err != nil {
		return nil, err
	}
	envelope, err := amadeus.Do[[]dto.AutocompleteLocation](ctx, s.client, amadeus.Request{
		Path:  pathByKeyword,
		Query: query.params(),
	})
	if err != nil {
		return nil, err
	}
	return mapSuggestions(envelope.Data), nil
}

// fetch is the one round trip the three searches share; they differ only in
// path and parameters.
func (s *service) fetch(ctx context.Context, path string, params url.Values) ([]Hotel, error) {
	envelope, err := amadeus.Do[[]dto.InventoryHotel](ctx, s.client, amadeus.Request{
		Path:  path,
		Query: params,
	})
	if err != nil {
		return nil, err
	}
	return mapHotels(envelope.Data), nil
}
