package inventory

import (
	"strconv"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/geo"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeus/dto"
)

// mapHotels translates a page of wire records into domain hotels.
func mapHotels(wire []dto.InventoryHotel) []Hotel {
	if wire == nil {
		return nil
	}
	out := make([]Hotel, len(wire))
	for i, h := range wire {
		out[i] = mapHotel(h)
	}
	return out
}

// mapHotel translates one wire record.
//
// Amadeus omits fields it has no value for rather than sending empty ones, so
// the pointer fields here distinguish "absent" from "zero". That matters most
// for coordinates: 0,0 is a real point, and defaulting an unlocatable property
// to it would put it in the Atlantic.
func mapHotel(h dto.InventoryHotel) Hotel {
	hotel := Hotel{
		ID:              HotelID(h.HotelID),
		Name:            h.Name,
		ChainCode:       h.ChainCode,
		BrandCode:       h.BrandCode,
		MasterChainCode: h.MasterChainCode,
		DupeID:          mapDupeID(h.DupeID),
		IATACode:        h.IataCode,
		Rating:          h.Rating,
		Amenities:       mapAmenities(h.Amenities),
		LastUpdate:      h.LastUpdate,
	}

	if h.GeoCode != nil {
		hotel.Position = &geo.Coordinates{
			Latitude:  h.GeoCode.Latitude,
			Longitude: h.GeoCode.Longitude,
		}
	}
	if h.Address != nil {
		hotel.Address = &Address{
			Lines:       h.Address.Lines,
			PostalCode:  h.Address.PostalCode,
			CityName:    h.Address.CityName,
			StateCode:   h.Address.StateCode,
			CountryCode: h.Address.CountryCode,
		}
	}
	if h.Distance != nil {
		distance := geo.NewDistance(h.Distance.Value, geo.Unit(h.Distance.Unit))
		hotel.DistanceFromSearch = &distance
	}
	if h.Retailing != nil && h.Retailing.Sponsorship != nil {
		hotel.Sponsored = h.Retailing.Sponsorship.IsSponsored
	}

	return hotel
}

// mapDupeID renders the numeric wire value as a string, matching how Hotel
// Search sends the same concept. Zero means Amadeus sent none.
func mapDupeID(id int64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

// mapSuggestions translates the Hotel Name Autocomplete matches into domain
// suggestions.
func mapSuggestions(wire []dto.AutocompleteLocation) []Suggestion {
	if wire == nil {
		return nil
	}
	out := make([]Suggestion, len(wire))
	for i, location := range wire {
		out[i] = mapSuggestion(location)
	}
	return out
}

// mapSuggestion translates one autocomplete match. The wire ID and type are
// dropped: the location resource ID appears in no other hotel API, and type is
// always "location".
func mapSuggestion(location dto.AutocompleteLocation) Suggestion {
	suggestion := Suggestion{
		Name:      location.Name,
		SubType:   codes.HotelSubType(location.SubType),
		IATACode:  location.IataCode,
		Relevance: location.Relevance,
	}

	if len(location.HotelIDs) > 0 {
		suggestion.HotelIDs = make([]HotelID, len(location.HotelIDs))
		for i, id := range location.HotelIDs {
			suggestion.HotelIDs[i] = HotelID(id)
		}
	}
	if location.GeoCode != nil {
		suggestion.Position = &geo.Coordinates{
			Latitude:  location.GeoCode.Latitude,
			Longitude: location.GeoCode.Longitude,
		}
	}
	if location.Address != nil {
		suggestion.Address = &Address{
			CityName:    location.Address.CityName,
			StateCode:   location.Address.StateCode,
			CountryCode: location.Address.CountryCode,
		}
	}

	return suggestion
}

// mapAmenities turns the amenity codes Hotel List echoes back into typed codes,
// leaving nil when Amadeus sent none so "not reported" stays distinct from
// "reported as empty".
func mapAmenities(wire []string) []codes.Amenity {
	if len(wire) == 0 {
		return nil
	}
	out := make([]codes.Amenity, len(wire))
	for i, a := range wire {
		out[i] = codes.Amenity(a)
	}
	return out
}
