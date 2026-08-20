package inventory

import (
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/geo"
)

// Suggestion is one hotel whose name matched a typed keyword, as Hotel Name
// Autocomplete reports it.
//
// It is not a Hotel: autocomplete matches names, so it carries no chain codes,
// no star rating and no distances - just enough to render a suggestion row and
// the property codes to continue with once the user picks it.
type Suggestion struct {
	// Name is the hotel name that matched the keyword.
	Name string
	// HotelIDs are the property codes behind this suggestion, and the key the
	// other contexts take. Usually one; a leisure property duplicated across
	// sources lists each duplicate's code.
	HotelIDs []HotelID
	// SubType is which inventory the match came from: aggregators
	// (codes.HotelSubTypeLeisure) or chains (codes.HotelSubTypeGDS).
	SubType codes.HotelSubType
	// IATACode is the city or airport code the property is filed under.
	IATACode string
	// Relevance ranks how well the name matched the keyword, 1 to 100 with
	// higher better. Amadeus already returns suggestions best-first, so this is
	// for display or re-ranking, not something a caller must sort by.
	Relevance int
	// Position is the property's coordinates, or nil when Amadeus sent none. A
	// pointer for the same reason as Hotel.Position: 0,0 is a real point, not
	// "unknown".
	Position *geo.Coordinates
	// Address is the city, state and country, or nil when Amadeus sent none.
	// Autocomplete never sends street lines or a postal code, so those fields
	// stay empty here.
	Address *Address
}

// IDs returns the suggestion's property codes as strings, which is the form
// the offers and content contexts take.
//
//	suggestions, _ := client.Inventory.ByKeyword(ctx, inventory.KeywordQuery{Keyword: "PARI"})
//	offers, _ := client.Offers.Search(ctx, offers.SearchQuery{
//	    HotelIDs: suggestions[0].IDs(),
//	    ...
//	})
func (s Suggestion) IDs() []string {
	out := make([]string, len(s.HotelIDs))
	for i, id := range s.HotelIDs {
		out[i] = string(id)
	}
	return out
}
