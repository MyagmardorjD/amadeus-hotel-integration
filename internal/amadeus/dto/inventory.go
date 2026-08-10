// Package dto holds the Amadeus wire structures, exactly as the API sends and
// receives them.
//
// Nothing here is part of the SDK's public API. These types exist to be decoded
// into and then translated by a context's mapper, and the internal/ path makes
// that unenforceable-by-convention rule a compile-time one. Field names, types
// and oddities mirror Amadeus even where they are inconsistent - see DupeID on
// InventoryHotel, which arrives as a number here and as a string from Hotel
// Search.
package dto

// InventoryHotel is one element of the Hotel List (v1.2) data array, returned
// by the by-city, by-geocode and by-hotels endpoints.
type InventoryHotel struct {
	ChainCode       string `json:"chainCode"`
	BrandCode       string `json:"brandCode"`
	MasterChainCode string `json:"masterChainCode"`
	IataCode        string `json:"iataCode"`
	// DupeID identifies properties that are duplicated across sources. Hotel
	// List sends it as a JSON number while Hotel Search sends the same concept
	// as a string; the domain normalises both to a string.
	DupeID  int64  `json:"dupeId"`
	Name    string `json:"name"`
	HotelID string `json:"hotelId"`
	// Rating is the property's star rating. Amadeus returns it on every hotel
	// when the search filters on ratings, and omits it otherwise.
	Rating int `json:"rating,omitempty"`
	// Amenities are the amenity codes Amadeus echoes back when the search
	// filters on amenities.
	Amenities  []string          `json:"amenities,omitempty"`
	GeoCode    *GeoCode          `json:"geoCode,omitempty"`
	Address    *InventoryAddress `json:"address,omitempty"`
	Distance   *Distance         `json:"distance,omitempty"`
	LastUpdate string            `json:"lastUpdate"`
	Retailing  *Retailing        `json:"retailing,omitempty"`
}

// GeoCode is Amadeus's latitude/longitude pair.
type GeoCode struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// InventoryAddress is the postal address shape used by Hotel List.
type InventoryAddress struct {
	Lines       []string `json:"lines"`
	PostalCode  string   `json:"postalCode"`
	CityName    string   `json:"cityName"`
	CountryCode string   `json:"countryCode"`
	StateCode   string   `json:"stateCode"`
}

// Distance is a length with its unit, as returned beside a searched location.
type Distance struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// Retailing carries merchandising metadata, currently only sponsorship.
type Retailing struct {
	Sponsorship *Sponsorship `json:"sponsorship,omitempty"`
}

// Sponsorship marks a property whose placement was paid for.
type Sponsorship struct {
	IsSponsored bool `json:"isSponsored"`
}
