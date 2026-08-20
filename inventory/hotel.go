// Package inventory answers "which hotels exist, and where".
//
// It is the bounded context over Amadeus's Hotel List API, plus the sibling
// Hotel Name Autocomplete API that ByKeyword wraps. Its concern is locating
// properties, not describing them and not pricing them: a Hotel here carries
// an identity, a position and enough address to show on a map. For a
// property's rooms, facilities and photographs use the content context; for
// what a stay costs use the offers context.
//
// The Amadeus name for the main API is "Hotel List". The context is called
// inventory because "list" names a mechanism rather than a concept, and would
// collide with the verb on every other service.
package inventory

import (
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/geo"
)

// HotelID is an Amadeus 8-character property code, e.g. "MCLONGHM": two
// characters of chain, three of city, three of property.
type HotelID string

// String returns the code.
func (id HotelID) String() string { return string(id) }

// IsValid reports whether id has the shape Amadeus issues. It checks the shape
// only; whether the property exists is Amadeus's answer to give.
func (id HotelID) IsValid() bool {
	if len(id) != 8 {
		return false
	}
	for _, r := range id {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// Hotel is a property as the inventory context knows it: identity, position and
// address.
type Hotel struct {
	// ID is the Amadeus property code, and the key every other context takes.
	ID HotelID
	// Name is the property name as Amadeus holds it.
	Name string

	// ChainCode is the two-letter chain, e.g. "MC" for Marriott.
	ChainCode string
	// BrandCode is the brand within the chain, where the chain has several.
	BrandCode string
	// MasterChainCode is the parent group, where the chain belongs to one.
	MasterChainCode string
	// DupeID groups records that describe the same physical property arriving
	// from more than one source. It is empty when Amadeus sends none.
	DupeID string

	// IATACode is the city or airport code the property is filed under.
	IATACode string

	// Rating is the property's star rating, 1 to 5. It is zero when Amadeus
	// sent none: Hotel List returns it on every hotel when the search filtered
	// on Ratings, and omits it entirely otherwise, so an unfiltered search
	// leaves this zero even for properties that do have stars.
	Rating int
	// Amenities are the amenity codes Amadeus echoed back, which it does when
	// the search filtered on Amenities. It is empty otherwise, and empty is not
	// evidence that a property lacks them - use the content context for the
	// full list.
	Amenities []codes.Amenity
	// Position is the property's coordinates, or nil when Amadeus could not
	// locate it. A pointer rather than a zero value, because 0,0 is a real
	// point in the Gulf of Guinea and is not the same as "unknown".
	Position *geo.Coordinates
	// Address is the postal address, or nil when Amadeus sends none.
	Address *Address

	// DistanceFromSearch is how far the property lies from the searched point,
	// and is set only by searches that have a centre: ByGeocode and ByCity. It
	// is nil for ByIDs, which has no centre to measure from.
	DistanceFromSearch *geo.Distance

	// Sponsored reports that the property's placement in these results was paid
	// for. Worth surfacing to users, and worth knowing before treating result
	// order as a ranking.
	Sponsored bool

	// LastUpdate is Amadeus's own timestamp for the record, in the format it
	// supplied. It is kept as a string because Amadeus is inconsistent about
	// whether it includes a time or a zone.
	LastUpdate string
}

// Address is a postal address as Hotel List reports it. Hotel Name
// Autocomplete reuses the type but never sends Lines or PostalCode.
type Address struct {
	// Lines is the street address, one element per line.
	Lines []string
	// PostalCode is the postal or ZIP code.
	PostalCode string
	// CityName is the city, spelled as Amadeus holds it.
	CityName string
	// StateCode is the state or province, for countries that use one.
	StateCode string
	// CountryCode is the ISO 3166-1 alpha-2 country code.
	CountryCode string
}

// IsEmpty reports whether the address carries no usable information, which
// happens for properties Amadeus holds only by coordinates.
func (a Address) IsEmpty() bool {
	return len(a.Lines) == 0 && a.PostalCode == "" && a.CityName == "" &&
		a.StateCode == "" && a.CountryCode == ""
}

// IDs returns the property codes of hotels, which is the form the offers and
// content contexts take.
//
//	hotels, _ := client.Inventory.ByCity(ctx, inventory.CityQuery{CityCode: "PAR"})
//	offers, _ := client.Offers.Search(ctx, offers.SearchQuery{
//	    HotelIDs: inventory.IDs(hotels),
//	    ...
//	})
func IDs(hotels []Hotel) []string {
	out := make([]string, len(hotels))
	for i, h := range hotels {
		out[i] = string(h.ID)
	}
	return out
}
