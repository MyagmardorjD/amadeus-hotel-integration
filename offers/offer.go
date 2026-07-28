// Package offers answers "what does a stay at this hotel cost, and can I book
// it".
//
// It is the bounded context over Amadeus's Hotel Search API. The central term
// is the Offer: a bookable rate for a room over a date range, at a price, under
// a set of policies. An offer is not a room. One physical room appears in many
// offers that differ by rate code, board type, cancellation policy and price -
// which is why GroupByRoom exists, and why a room-picker UI needs it.
//
// The offer ID produced here is what the booking context takes to make a
// reservation. Offer IDs are short-lived: Amadeus expires them, and a stale one
// fails at booking time rather than at search time.
package offers

import (
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/datetime"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/geo"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/media"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/money"
)

// OfferID identifies a bookable offer. It is the value the booking context
// takes, and it expires: Amadeus invalidates offer IDs after a short window, so
// treat one as good for the current user session and no longer.
type OfferID string

// String returns the identifier.
func (id OfferID) String() string { return string(id) }

// HotelOffers is one hotel together with its bookable offers, and is what a
// search returns per property.
type HotelOffers struct {
	// Hotel identifies and locates the property.
	Hotel Hotel
	// Available reports whether the property has bookable inventory for the
	// requested stay. A hotel with no availability is still returned when the
	// search asked to include closed properties, with Offers empty.
	Available bool
	// Offers are the bookable rates, in the order Amadeus returned them. With
	// the default BestRateOnly this holds the single cheapest offer.
	Offers []Offer
	// Self is the Amadeus URL that reproduces this result.
	Self string

	// Rates are the currency conversions the search returned, present when
	// SearchQuery.Currency asked for a currency the hotels do not quote in.
	//
	// Amadeus does not convert the prices: it returns them in the hotel's own
	// currency and supplies the rate. Applying it is the caller's job, and
	// Rates.Convert does it. This is response-level data, so every hotel in
	// one search carries the same map.
	Rates ConversionRates
}

// Display converts any amount from this result into the currency the search
// asked for, ready to show to a user.
//
// It never mutates the offer: offer.Price stays in the hotel's own currency,
// which is the price the booking is charged in and the figure that reconciles
// against Amadeus. Display produces a separate value for the screen. Use it for
// any money on the offer - the total, the base, a tax, a per-night rate:
//
//	shown := result.Display(offer.Price.Total)
//	fmt.Println(shown)              // "5782360 MNT", or the original if no rate
//	if shown.Converted { ... }      // label the currency accordingly
//
// When no rate covers the amount's currency the original is returned unchanged,
// with Converted false, so the caller always has something displayable and
// always knows which currency it is in.
func (h HotelOffers) Display(amount money.Money) DisplayMoney {
	converted, ok := h.Rates.ConvertOrOriginal(amount)
	return DisplayMoney{
		Amount:    converted,
		Original:  amount,
		Converted: ok,
	}
}

// DisplayTotal is the common case: the amount to show the guest, converted into
// the requested currency.
//
// It displays offer.Price.Payable(), not Total, so an agency markup is included
// when one applies and a zero SellingTotal is never mistaken for the price.
func (h HotelOffers) DisplayTotal(offer Offer) DisplayMoney {
	return h.Display(offer.Price.Payable())
}

// DisplayMoney is an amount prepared for the screen: converted into the
// currency the search asked for when a rate was available, and left in its
// original currency when not.
//
// It carries the original alongside so nothing is lost. Amount is what to show
// the user; Original is what the booking is actually charged.
type DisplayMoney struct {
	// Amount is the figure to display, in the target currency when Converted
	// is true and in the original currency otherwise.
	Amount money.Money
	// Original is the price exactly as Amadeus quoted it - the source of truth
	// for booking, never a converted estimate.
	Original money.Money
	// Converted reports whether Amount was converted. When false, Amount equals
	// Original; use it to choose the right currency label.
	Converted bool
}

// String renders the display amount, e.g. "5782360 MNT".
func (d DisplayMoney) String() string { return d.Amount.String() }

// Currency returns the currency of the display amount.
func (d DisplayMoney) Currency() money.Currency { return d.Amount.Currency() }

// Cheapest returns the lowest-priced offer, and false when the hotel has none.
// Offers with an unparseable price are skipped rather than winning by default.
//
// Prices are compared in the response's target currency, so a hotel quoting one
// room in several currencies still yields the genuinely cheapest offer rather
// than the first one seen. See ConversionRates.comparePrices.
func (h HotelOffers) Cheapest() (Offer, bool) {
	var best Offer
	found := false

	for _, offer := range h.Offers {
		if offer.Price.Total.Amount().IsZero() {
			continue
		}
		if !found {
			best, found = offer, true
			continue
		}
		if h.Rates.comparePrices(offer.Price.Total, best.Price.Total) < 0 {
			best = offer
		}
	}

	// A hotel whose every offer lacks a usable price still has offers; return
	// the first rather than claiming there are none.
	if !found && len(h.Offers) > 0 {
		return h.Offers[0], true
	}
	return best, found
}

// Hotel is the offers context's view of a property: enough to identify and
// place it beside a price. For a full description - rooms, facilities,
// photographs - use the content context.
type Hotel struct {
	// ID is the Amadeus 8-character property code.
	ID string
	// Name is the property name.
	Name string
	// ChainCode is the two-letter chain, and BrandCode the brand within it.
	ChainCode string
	BrandCode string
	// DupeID groups records describing the same physical property from
	// different sources.
	DupeID string
	// CityCode is the IATA city code the property is filed under.
	CityCode string
	// Rating is the star rating, empty when Amadeus holds none.
	Rating codes.Rating
	// Position is the property's coordinates, nil when Amadeus sent none.
	Position *geo.Coordinates
	// Address is the postal address, nil when Amadeus sent none.
	Address *Address
	// Contact holds the property's phone, fax and email, nil when absent.
	Contact *Contact
	// AmenityCodes are the property's amenities as Amadeus's content codes.
	// These are the content vocabulary, not the codes.Amenity search filters,
	// so they are kept as strings rather than mistyped as the filter set.
	AmenityCodes []string
	// TermsAndConditions links to the terms a guest must accept to book.
	TermsAndConditions string
}

// Address is a postal address.
type Address struct {
	Lines       []string
	PostalCode  string
	CityName    string
	StateCode   string
	CountryCode string
}

// Contact holds a property's contact details.
type Contact struct {
	Phone string
	Fax   string
	Email string
}

// Offer is one bookable rate: a room, for a stay, at a price, under policies.
type Offer struct {
	// ID is what the booking context takes to reserve this rate.
	ID OfferID
	// Stay is the date range this price covers.
	Stay Stay
	// Guests is the occupancy the price was quoted for.
	Guests Guests
	// RoomQuantity is how many rooms this offer covers. Zero means Amadeus did
	// not say, which conventionally means one.
	RoomQuantity int

	// Room describes what is being booked.
	Room Room
	// RoomDetails is Amadeus's richer room block, present on some sources only.
	RoomDetails *RoomDetails
	// StandardizedRoom is Amadeus's normalised room description, which is
	// comparable across properties where Room is not.
	StandardizedRoom *StandardizedRoom

	// Price is the cost of the stay.
	Price Price
	// Policies govern cancellation, payment, deposits and length of stay. Read
	// Policies.Cancellation before presenting an offer as refundable.
	Policies Policies

	// BoardType is the meals included in the rate.
	BoardType codes.BoardType
	// RateCode is the Amadeus 3-character rate code, e.g. "PRO" or a corporate
	// code.
	RateCode codes.RateCode
	// RateFamily is Amadeus's estimated classification of the rate.
	RateFamily *RateFamily
	// RateName is the marketing name of the rate.
	RateName string
	// IsLoyaltyRate reports a rate requiring loyalty membership. Amadeus sends
	// this as the string "true"/"false"; the mapper normalises it.
	IsLoyaltyRate bool
	// PromotionCode is the promotion applied, when one was.
	PromotionCode *PromotionCode
	// Category is Amadeus's offer category.
	Category string
	// Description is the offer's free-text description.
	Description *media.Text
	// Commission is what the booker earns, on rates that pay one.
	Commission *Commission
	// Extras are the chargeable or complimentary additions attached to the
	// offer, such as breakfast or parking. Amadeus calls these "services".
	Extras []Extra
	// ProviderReference links the offer to the content provider's own record.
	ProviderReference *ProviderReference
	// Self is the Amadeus URL for this offer.
	Self string
}

// Stay is the date range an offer covers.
type Stay struct {
	CheckIn  datetime.Date
	CheckOut datetime.Date
}

// Nights returns the number of nights in the stay, and zero when either date is
// missing.
func (s Stay) Nights() int {
	if s.CheckIn.IsZero() || s.CheckOut.IsZero() {
		return 0
	}
	return s.CheckIn.DaysUntil(s.CheckOut)
}

// IsZero reports whether the stay has no dates.
func (s Stay) IsZero() bool { return s.CheckIn.IsZero() && s.CheckOut.IsZero() }

// Guests is the occupancy a price was quoted for.
type Guests struct {
	// Adults is the number of adults per room.
	Adults int
	// ChildAges lists each child's age at check-out. Amadeus prices children
	// by age, so two children of different ages are two entries, and the
	// length of this slice is the number of children.
	ChildAges []int
}

// Children returns the number of children in the party.
func (g Guests) Children() int { return len(g.ChildAges) }

// Total returns the number of people the offer covers.
func (g Guests) Total() int { return g.Adults + len(g.ChildAges) }

// Room is the room an offer books, as Hotel Search describes it.
type Room struct {
	// Type is the property's own room code, e.g. "C3S". It is the key
	// GroupByRoom groups on: offers sharing a Type are the same room at
	// different rates.
	Type string
	// Category is Amadeus's estimated category, e.g. "STANDARD_ROOM".
	Category string
	// Beds is the number of beds, and BedType their kind, e.g. "KING".
	Beds    int
	BedType string
	// Description is the property's free-text description of the room.
	Description *media.Text
}

// RoomDetails is Amadeus's extended room block. It is present only for sources
// that supply it, which is why every field is optional.
type RoomDetails struct {
	ID          string
	Name        *media.Text
	Type        string
	Description string

	Category       string
	Classification string
	Location       string
	Architecture   string
	ViewCode       string

	Beds             int
	BedType          string
	BedroomsPerRoom  int
	BathroomsPerRoom int
	Quantity         int
	SortOrder        int

	Dimensions       *media.Dimensions
	MaxOccupancy     *Occupancy
	SleepFurnishings *SleepFurnishings

	Amenities          []RoomAmenity
	Media              []media.Asset
	PolicyDescriptions []media.Text
}

// StandardizedRoom is Amadeus's normalised room description, comparable across
// properties in a way each property's own room codes are not.
type StandardizedRoom struct {
	ID                string
	Name              string
	Amenities         []StandardizedAmenity
	Views             []StandardizedView
	BedConfigurations []BedConfiguration
	Dimensions        *media.Dimensions
	MaxOccupancy      *Occupancy
}

// StandardizedAmenity is a normalised amenity code with its description.
type StandardizedAmenity struct {
	Code        string
	Description string
}

// StandardizedView is a normalised room view, e.g. sea or city.
type StandardizedView struct {
	Code        string
	Description string
}

// BedConfiguration describes one arrangement of beds in a room.
type BedConfiguration struct {
	// Beds is Amadeus's description of the bed count for this arrangement.
	Beds string
	// Attributes is the free-form bed block Amadeus supplies, whose shape it
	// does not document. It is preserved rather than dropped, but it is the one
	// place the wire format shows through.
	Attributes map[string]any
}

// Occupancy is how many people a room takes.
type Occupancy struct {
	Adults   int
	Children int
	Total    int
}

// SleepFurnishings is the extra sleeping furniture a room can supply.
type SleepFurnishings struct {
	Cribs     int
	ExtraBeds int
}

// RoomAmenity is an amenity attached to a room, with its price when charged for.
type RoomAmenity struct {
	Code                  string
	Description           string
	Type                  string
	Attribute             string
	QualityAssessment     string
	PerformanceAssessment string
	Provider              string
	PricingMethod         string
	Quantity              int
	Price                 *ExtraPrice
	Media                 []media.Asset
}

// Extra is one chargeable or complimentary addition to an offer - breakfast,
// parking, a spa credit. Amadeus calls these "services"; the domain calls them
// extras, which is both the standard hospitality term and unambiguous against
// the Service interface.
type Extra struct {
	Code          string
	Description   string
	IsChargeable  bool
	PricingMethod string
	Quantity      int
	Attribute     string
	Price         *ExtraPrice
}

// ExtraPrice is the price of an extra or a room amenity.
type ExtraPrice struct {
	Currency     money.Currency
	Base         money.Money
	Total        money.Money
	SellingTotal money.Money
	Taxes        []Tax
	Markups      []money.Money
	Variations   *Variations
}

// RateFamily is Amadeus's estimated classification of a rate.
type RateFamily struct {
	// Code is the 3-character family code, e.g. "PRO", "FAM", "GOV".
	Code string
	// Type is P for public, N for negotiated, C for conditional.
	Type string
}

// PromotionCode is a promotion applied to a rate.
type PromotionCode struct {
	Code        string
	Description string
}

// ProviderReference links an offer to the content provider's own record.
type ProviderReference struct {
	ID  string
	Ref string
}

// Commission is what the booker earns on an offer.
type Commission struct {
	// Amount is the flat commission, when the rate pays one.
	Amount money.Money
	// Percentage is the commission rate as Amadeus expressed it.
	Percentage string
	// Description explains the commission terms.
	Description *media.Text
}
