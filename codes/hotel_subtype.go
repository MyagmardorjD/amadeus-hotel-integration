package codes

// HotelSubType selects which inventory the Hotel Name Autocomplete API matches
// a keyword against, via the `subType` query parameter. Unlike the other
// filters this parameter is repeated rather than comma-joined, and Amadeus
// requires at least one value; the inventory context sends both when the caller
// picks neither.
type HotelSubType string

const (
	// HotelSubTypeLeisure matches aggregator (leisure) inventory.
	HotelSubTypeLeisure HotelSubType = "HOTEL_LEISURE"
	// HotelSubTypeGDS matches chains connected directly to Amadeus.
	HotelSubTypeGDS HotelSubType = "HOTEL_GDS"
)

var hotelSubTypeCatalog = []entry[HotelSubType]{
	{HotelSubTypeLeisure, "Aggregators"},
	{HotelSubTypeGDS, "GDS / Distribution"},
}

// AllHotelSubTypes returns every autocomplete sub type Amadeus accepts.
func AllHotelSubTypes() []HotelSubType { return allOf(hotelSubTypeCatalog) }

// Label returns a human-readable name for h, or "" when h is not a known code.
func (h HotelSubType) Label() string { return labelOf(hotelSubTypeCatalog, h) }

// IsValid reports whether h is a code Amadeus accepts.
func (h HotelSubType) IsValid() bool { return isValid(hotelSubTypeCatalog, h) }
