// Package content describes a hotel: its rooms, facilities, policies,
// photographs and surroundings.
//
// It is the bounded context over Amadeus's Hotel Content API. Its concern is
// what a property is like, not where it is (that is inventory) and not what it
// costs (that is offers). Nothing here is priced or bookable, and nothing here
// changes per stay - which is why content is worth caching and an offer is not.
//
// Almost every field is optional. What a property publishes varies enormously
// by source: a chain hotel may return rooms, facilities, awards and fifty
// photographs, while an aggregator listing returns a name and an address. The
// pointer and slice fields below are absent far more often than the schema
// suggests, so check before dereferencing.
package content

import (
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/datetime"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/geo"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/media"
)

// Hotel is everything Amadeus publishes about a property.
type Hotel struct {
	// ID is the Amadeus 8-character property code.
	ID string
	// Name is the property name.
	Name string
	// ChainCode and BrandCode identify the chain and brand, with ChainName and
	// BrandName their display forms.
	ChainCode string
	BrandCode string
	ChainName string
	BrandName string
	// DupeID groups records describing the same physical property across
	// providers.
	DupeID string

	// Rating is the star rating.
	Rating codes.Rating
	// Status is the property's operational state, e.g. whether it is open.
	Status string
	// Description is the property's long prose description.
	//
	// Amadeus does not send this as a field of its own. It arrives as an entry
	// in the media array carrying text but no image, tagged with what the text
	// describes. The mapper pulls the long description out; everything else is
	// in Descriptions.
	Description *media.Text
	// Descriptions holds every prose block the property published, with Type
	// set to Amadeus's tag: HOTEL_LONG_DESCRIPTION, HOTEL_SHORT_DESCRIPTION,
	// LONG_LOCATION_DESCRIPTION, MARKETING, AREAS_SERVED and others.
	// DescriptionOf looks one up.
	Descriptions []media.Text

	// Amenities are the property-level amenities, with their descriptions and
	// any charges.
	Amenities []Amenity
	// Media are the property's photographs. Prose-only entries are filtered
	// out into Descriptions, so everything here carries an actual image. Use
	// media.Asset.Best to pick a rendition rather than downloading originals.
	Media []media.Asset

	// Location places the property.
	Location *Location
	// Contacts are the property's addresses, phone numbers and websites, one
	// entry per purpose.
	Contacts []Contact
	// Areas are the geographic zones the property belongs to.
	Areas []Area

	// Categories and Segments are Amadeus's classifications of the property.
	Categories []string
	Segments   []string
	// CategoryCode is the primary category code.
	CategoryCode string

	// DefaultLanguage is the language spoken at the property by default, and
	// SpokenLanguages everything its staff speak.
	DefaultLanguage string
	SpokenLanguages []string
	// Currencies are the currencies the property accepts.
	Currencies []string

	// OpenPeriods are the seasons the property is open, and are set only for
	// properties that close for part of the year.
	OpenPeriods []Period

	// TaxID and BusinessIdentifiers are the property's registration numbers.
	TaxID               string
	BusinessIdentifiers []Identifier

	// TimeZone is the property's local zone, which matters because check-in
	// dates are local to the hotel.
	TimeZone *TimeZone
	// Climate describes the local climate.
	Climate string
	// Building describes the physical structure.
	Building *Building
	// Altitude is the property's elevation.
	Altitude *Altitude

	// Rooms are the room types the property publishes.
	Rooms []Room
	// Facilities are the meeting rooms, restaurants and amenities on site.
	Facilities *Facilities
	// Policies are the property's rules: payment, check-in, pets, taxes and so
	// on. These are property-level policies, distinct from the per-offer
	// policies attached to a bookable rate.
	Policies *Policies
	// Awards are the ratings and certifications the property holds.
	Awards []Award
	// Certifications are awards Amadeus files separately under the hotel block.
	Certifications []Award
	// Promotions are the property's current marketing offers. These are not
	// bookable: use the offers context for a price.
	Promotions []Promotion
	// PointsOfInterest are notable places near the property.
	PointsOfInterest []PointOfInterest
	// NearbyLandmarks are the distances Amadeus publishes to nearby places.
	NearbyLandmarks []Landmark
}

// Position returns the property's coordinates, and false when Amadeus published
// none.
func (h Hotel) Position() (geo.Coordinates, bool) {
	if h.Location == nil || h.Location.Position == nil {
		return geo.Coordinates{}, false
	}
	return *h.Location.Position, true
}

// PrimaryPhoto returns the first asset that actually carries an image, which is
// the one to lead with in a listing. It reports false when the property
// published no photographs.
//
// The IsVisual check is load-bearing: Amadeus ships prose in the same array as
// photographs, and returning a text block here would give you an empty src.
func (h Hotel) PrimaryPhoto() (media.Asset, bool) {
	for _, asset := range h.Media {
		if asset.IsVisual() {
			return asset, true
		}
	}
	return media.Asset{}, false
}

// DescriptionOf returns the prose block Amadeus tagged with the given type,
// e.g. "HOTEL_SHORT_DESCRIPTION" or "LONG_LOCATION_DESCRIPTION".
func (h Hotel) DescriptionOf(descriptionType string) (media.Text, bool) {
	for _, text := range h.Descriptions {
		if text.Type == descriptionType {
			return text, true
		}
	}
	return media.Text{}, false
}

// HasAmenity reports whether the property publishes the given amenity code.
func (h Hotel) HasAmenity(code string) bool {
	for _, amenity := range h.Amenities {
		if amenity.Code == code {
			return true
		}
	}
	return false
}

// Amenity is something a property or room offers, with its price when charged
// for.
type Amenity struct {
	// Code identifies the amenity, e.g. "SWIMMING_POOL".
	Code string
	// Description explains it in prose.
	Description string
	// Type and Attribute classify it, e.g. type "WIFI" attribute "IN_ROOM".
	Type      string
	Attribute string
	// QualityAssessment and PerformanceAssessment are Amadeus's ratings of it,
	// e.g. "Standard", or a wifi bandwidth indicator.
	QualityAssessment     string
	PerformanceAssessment string
	// Provider is who supplied the amenity data, e.g. "ATPCO".
	Provider string
	// IsChargeable reports that using it costs extra.
	IsChargeable bool
	// PricingMethod is how the charge is assessed, e.g. "PER_ROOM_PER_NIGHT".
	PricingMethod string
	// Quantity is how many are available.
	Quantity int
	// Media are photographs of the amenity.
	Media []media.Asset
}

// Location places a property or point of interest.
type Location struct {
	// Name is the place's label.
	Name string
	// SubType classifies it, e.g. "CITY", "AIRPORT".
	SubType string
	// IATACode is the city or airport code.
	IATACode string
	// Position is the coordinates, nil when Amadeus published none.
	Position *geo.Coordinates
}

// Area is a geographic zone a property belongs to, such as a city or district.
type Area struct {
	// Type classifies the zone, e.g. "CITY", "DISTRICT".
	Type string
	// Name is its label, e.g. "Montmartre".
	Name string
}

// Landmark is a place near the property, with how far away it is.
type Landmark struct {
	// Name is the landmark, e.g. "Eiffel Tower".
	Name string
	// Type classifies it.
	Type string
	// Distance is how far the property lies from it.
	Distance *geo.Distance
	// Direction is the compass bearing, when Amadeus supplies one.
	Direction string
}

// Contact is one way to reach the property, for one purpose.
type Contact struct {
	// Purposes are what this contact is for, e.g. "RESERVATIONS".
	Purposes []string
	// LocationType classifies the address, e.g. "PHYSICAL", "MAILING".
	LocationType string
	// AddresseeName is the person or department addressed.
	AddresseeName string
	// Address is the postal address.
	Address *Address
	// Phones are the telephone and fax numbers.
	Phones []Phone
	// Emails are the email addresses.
	Emails []string
	// Website is the URL.
	Website string
}

// Address is a postal address.
type Address struct {
	Lines       []string
	PostalCode  string
	CityName    string
	StateCode   string
	CountryCode string
	CountryName string
	PostalBox   string
}

// Phone is a telephone or fax number with its category.
type Phone struct {
	// Number is the number as published.
	Number string
	// Category classifies it, e.g. "PHONE", "FAX".
	Category string
	// Type further classifies it, e.g. "VOICE", "MOBILE".
	Type string
}

// Period is a date range, used for the seasons a property is open.
type Period struct {
	Start datetime.Date
	End   datetime.Date
}

// Identifier is a business registration number.
type Identifier struct {
	ID   string
	Name string
}

// TimeZone is the property's local timezone.
type TimeZone struct {
	// Name is the zone identifier or label Amadeus publishes.
	Name string
	// OffsetHours is the offset from UTC, when supplied.
	OffsetHours string
	// DaylightSaving reports whether the zone observes it.
	DaylightSaving bool
}

// Building describes the property's physical structure.
type Building struct {
	// Floors is the number of storeys.
	Floors int
	// TotalRooms is how many rooms the property has.
	TotalRooms int
	// ExecutiveFloors, Buildings and Elevators are the rest of what Amadeus
	// counts about the structure. All three were decoded and then dropped.
	ExecutiveFloors int
	Buildings       int
	Elevators       int
	// YearBuilt and YearRenovated are the construction and last-renovation
	// years, as Amadeus published them.
	YearBuilt     string
	YearRenovated string
	// Architecture is the style Amadeus files the building under, e.g.
	// "MODERN". It used to be returned as Description, which is a different
	// thing: a client rendering prose got a code word.
	Architecture string
	// Description is prose about the building. Amadeus publishes none in this
	// block today, so it is reserved rather than filled from something else.
	Description string
}

// Altitude is an elevation with its unit.
type Altitude struct {
	Value int
	Unit  string
}

// Room is a room type the property publishes.
//
// This is a catalogue entry, not a bookable room: it describes what the
// property has, with no price and no availability. For a bookable room use the
// offers context.
type Room struct {
	// Name and Description are the property's own text for the room.
	Name        *media.Text
	Description *media.Text

	// Classification and Category are Amadeus's normalisations, e.g. "ROOM"
	// and "DELUXE".
	Classification string
	Category       string
	// Location describes where in the property the room sits.
	Location string
	// Architecture is the style code.
	Architecture string
	// ViewCode is what the room looks out on, e.g. "OCEAN".
	ViewCode string

	// Beds is the number of beds and BedType their kind.
	Beds    int
	BedType string
	// Bedrooms and Bathrooms count the rooms within the unit.
	Bedrooms  int
	Bathrooms int
	// Quantity is how many of this room type the property has.
	Quantity int
	// SortOrder is the property's own display ordering.
	SortOrder int

	// IsNonSmoking reports a non-smoking room.
	IsNonSmoking bool
	// StandardOccupancy is the occupancy the room is designed for, and
	// MaxOccupancy the most it takes.
	StandardOccupancy int
	MaxOccupancy      *Occupancy
	// SleepFurnishings is the extra bedding available.
	SleepFurnishings *SleepFurnishings
	// Dimensions is the room's size.
	Dimensions *media.Dimensions

	// Amenities are the room's own amenities.
	Amenities []Amenity
	// Media are photographs of the room. Prose-only entries are filtered out
	// into Descriptions, so everything here carries an actual image, exactly
	// as on Hotel.Media.
	//
	// Many properties publish no room photography at all: Amadeus attaches it
	// to the property instead, or to a nameless room entry, so a named
	// bookable room routinely has none. Fall back to Hotel.Media when this is
	// empty.
	Media []media.Asset
	// Descriptions holds the prose blocks that shared the room's media array,
	// with Type set to Amadeus's tag - DEFAULT_ROOM_NAME,
	// STANDARD_ROOM_CATEGORY and others.
	Descriptions []media.Text
	// PolicyDescriptions are room-specific rules.
	PolicyDescriptions []string
	// ProviderReference links to the content provider's record.
	ProviderReference *ProviderReference
}

// Occupancy is how many people a room takes.
type Occupancy struct {
	Adults   int
	Children int
	Total    int
}

// SleepFurnishings is the extra bedding a room can supply.
type SleepFurnishings struct {
	Cribs     int
	ExtraBeds int
	RollAways int
}

// ProviderReference links a record to the content provider's own.
type ProviderReference struct {
	ID   string
	Ref  string
	Name string
}

// Award is a rating or certification the property holds.
type Award struct {
	// Name is the award, e.g. "AAA Diamond".
	Name string
	// Provider is who granted it.
	Provider string
	// Rating is the level awarded.
	Rating string
	// RatingSystem names the scheme.
	RatingSystem string
	// Description explains it.
	Description string
	// DateGranted is when it was awarded, as Amadeus published it.
	DateGranted string
}

// Promotion is a marketing offer the property publishes.
//
// It is descriptive only: a promotion here is not bookable and carries no
// price. Search the offers context to find out what a stay actually costs.
type Promotion struct {
	Name        string
	Description string
	// Category classifies the promotion, e.g. "AAA".
	Category string
	// Code is the promotion code, where one applies.
	Code string
	// TermsAndConditions are the conditions attached.
	TermsAndConditions string
	// Media are the promotion's images.
	Media []media.Asset
}

// PointOfInterest is a notable place near the property.
type PointOfInterest struct {
	// Name is the place.
	Name string
	// Description explains it.
	Description string
	// CategoryCode classifies it, e.g. "SIGHTSEEING".
	CategoryCode string
	// Location places it.
	Location *Location
	// Distance is how far it lies from the property.
	Distance *geo.Distance
	// Contact is how to reach it.
	Contact *Contact
	// Website is its official URL.
	Website string
	// Season is when it is open, for seasonal attractions.
	Season *Period
	// Media are photographs of it.
	Media []media.Asset
	// Transportations are how to reach it - the modes Amadeus lists with the
	// journey time for each. Decoded and then dropped until now, which left a
	// point of interest with a distance and no way of getting there.
	Transportations []Transportation
}

// Transportation is one way of reaching a place.
type Transportation struct {
	// Mode is the means, e.g. "TAXI", "SUBWAY", "WALKING".
	Mode string
	// Description explains the journey.
	Description string
	// Hours is when the service runs, and ReservationRequired whether it has to
	// be booked ahead.
	Hours               *Schedule
	ReservationRequired bool
	// Media are photographs of it.
	Media []media.Asset
}

// Facilities are the shared amenities and venues on the property.
type Facilities struct {
	// Amenities are the property's shared facilities.
	Amenities []Amenity
	// MeetingRooms describe the conference and event space.
	MeetingRooms *MeetingRooms
	// Restaurants describe the dining on site.
	Restaurants *Restaurants
}

// MeetingRooms describes a property's event space.
type MeetingRooms struct {
	// Count is how many meeting rooms there are. Amadeus routinely states the
	// rooms without stating a count, so this falls back to len(Rooms).
	Count int
	// LargestCapacity is the biggest room's capacity.
	LargestCapacity int
	// TotalArea is the combined area, with its unit.
	TotalArea *media.Dimensions
	// Description is prose about the space, taken from the first room that has
	// any when Amadeus gives no summary of its own.
	Description string
	// Rooms are the individual spaces, in the order Amadeus published them.
	//
	// This is where the names and the photographs live. The fields above say how
	// much space there is; these say what it is called and what it looks like,
	// and a property page has nothing to show without them.
	Rooms []MeetingRoom
}

// MeetingRoom is one named event space.
type MeetingRoom struct {
	// Name is what the property calls it, e.g. "SORBONNE".
	Name string
	// Type is its layout classification, e.g. "BOARDROOM".
	Type string
	// Description is prose about this room. Amadeus files it either on the room
	// or inside its media, and both end up here.
	Description string
	// Capacities are the maximum occupancies per seating layout.
	Capacities []LayoutCapacity
	// Dimensions are the room's own measurements.
	Dimensions *media.Dimensions
	// Media are photographs of the room.
	Media []media.Asset
	// SortOrder is the position Amadeus wants it displayed in, zero when it
	// stated none.
	SortOrder int
}

// LayoutCapacity is how many people one seating layout holds.
type LayoutCapacity struct {
	// Layout is the arrangement, e.g. "U_SHAPE".
	Layout string
	// MaxOccupancy is how many it seats that way.
	MaxOccupancy int
}

// Restaurants describes a property's dining.
type Restaurants struct {
	// Count is how many restaurants there are. Amadeus routinely states the
	// venues without stating a count, so this falls back to len(Venues).
	Count int
	// Cuisines are the cuisine types served, across every venue.
	Cuisines []string
	// Description is prose about the dining, taken from the first venue that has
	// any when Amadeus gives no summary of its own.
	Description string
	// Venues are the individual restaurants and bars, in the order Amadeus
	// published them. This is where the names and the photographs live.
	Venues []Restaurant
}

// Restaurant is one named dining venue.
type Restaurant struct {
	// Name is what the property calls it, e.g. "BAR".
	Name string
	// Category is the service category, e.g. "BAR_OR_LOUNGE".
	Category string
	// Description is prose about this venue. Amadeus files it either on the
	// venue or inside its media, and both end up here.
	Description string
	// Cuisines are the cuisine types this venue serves.
	Cuisines []string
	// MaxSeating is how many it seats, zero when Amadeus stated none.
	MaxSeating int
	// ServesBreakfast, ServesBrunch, ServesLunch and ServesDinner are the meals
	// it serves.
	ServesBreakfast bool
	ServesBrunch    bool
	ServesLunch     bool
	ServesDinner    bool
	// Media are photographs of the venue.
	Media []media.Asset
	// Hours is when the venue is open. A restaurant nobody can tell the opening
	// times of is half a listing.
	Hours *Schedule
	// ReservationRequired reports whether a guest has to book a table.
	ReservationRequired bool
}

// Schedule is when something is open: the days of the week, the hours on those
// days, and the dates the schedule itself is in force.
type Schedule struct {
	// Days are the weekdays it applies to, e.g. "MON", "SAT".
	Days []string
	// StartTime and EndTime are the opening hours, as Amadeus published them.
	StartTime string
	EndTime   string
	// StartDate and EndDate bound a seasonal schedule; empty when it is
	// year-round.
	StartDate string
	EndDate   string
}

// IsEmpty reports whether a schedule says nothing at all.
func (s Schedule) IsEmpty() bool {
	return len(s.Days) == 0 && s.StartTime == "" && s.EndTime == "" &&
		s.StartDate == "" && s.EndDate == ""
}
