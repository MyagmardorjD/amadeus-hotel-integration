package offers

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/apierr"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/datetime"
)

// Limits Amadeus enforces on a search. Checking them here turns a round trip
// and an opaque 400 into an immediate error naming the field.
const (
	// MaxAdults is the most adults Amadeus prices per room.
	MaxAdults = 9
	// MaxRooms is the most rooms Amadeus prices in one request.
	MaxRooms = 9
	// MaxHotelIDs is the most property codes accepted in one search. The cap is
	// exactly 50: 50 is accepted, 51 is rejected with "477 INVALID FORMAT -
	// Exceeding max items for: hotelIds". It is lower than the 100 the
	// by-hotels inventory lookup takes, so the two are not interchangeable.
	MaxHotelIDs = 50
	// MaxChildAge is the oldest a guest can be and still be priced as a child.
	MaxChildAge = 17
)

// SearchQuery describes a hotel offers search.
//
// HotelIDs is the only required field; Amadeus defaults the rest. Get the IDs
// from the inventory context:
//
//	hotels, _ := client.Inventory.ByCity(ctx, inventory.CityQuery{CityCode: "PAR"})
//	results, _ := client.Offers.Search(ctx, offers.SearchQuery{
//	    HotelIDs: inventory.IDs(hotels)[:20],
//	    Stay:     offers.Stay{CheckIn: checkIn, CheckOut: checkOut},
//	    Guests:   offers.Guests{Adults: 2},
//	})
type SearchQuery struct {
	// HotelIDs are the Amadeus 8-character property codes to price. Required,
	// at most MaxHotelIDs of them.
	HotelIDs []string

	// Stay is the date range to price. When zero, Amadeus prices tonight for
	// one night. Dates are the hotel's local dates, not the caller's.
	Stay Stay
	// Guests is the occupancy. A zero Adults means Amadeus's default of one.
	Guests Guests
	// Rooms is how many rooms to price. Zero means one.
	Rooms int

	// BoardType filters by the meals included.
	BoardType codes.BoardType
	// RateCodes filters by special rate. Corporate codes are account-specific
	// and are accepted here even though codes.AllRateCodes cannot list them.
	RateCodes []codes.RateCode
	// PaymentPolicy filters by payment type. Empty returns every type.
	PaymentPolicy codes.PaymentPolicy

	// Currency requests prices in a specific ISO currency. A property that
	// cannot quote it returns its own currency instead, so do not assume the
	// response matches.
	Currency string
	// PriceRange filters by price per night, as Amadeus's own range syntax:
	// "200-300", "-300" or "100". Setting it requires setting Currency.
	PriceRange string
	// CountryOfResidence is the guest's ISO 3166-1 country, which affects
	// which rates and taxes apply.
	CountryOfResidence string
	// BestRateOnly returns only the cheapest offer per hotel. Amadeus defaults
	// to true; set it to codes.Ptr(false) to get every rate, which is what
	// GroupByRoom needs to be useful.
	BestRateOnly *bool
	// IncludeClosed returns sold-out properties too, with no offers attached.
	IncludeClosed *bool

	// Lang requests descriptive text in a language, e.g. "FR". Amadeus falls
	// back to English where it holds no translation.
	Lang string
	// PageOffset resumes a paged search from a previous response.
	PageOffset string
}

func (q SearchQuery) validate() error {
	var errs apierr.ValidationErrors

	switch {
	case len(q.HotelIDs) == 0:
		errs = errs.Append("HotelIDs", "at least one hotel ID is required")
	case len(q.HotelIDs) > MaxHotelIDs:
		errs = append(errs, apierr.Invalidf("HotelIDs",
			"at most %d IDs per search, got %d", MaxHotelIDs, len(q.HotelIDs)))
	}
	for _, id := range q.HotelIDs {
		if strings.TrimSpace(id) == "" {
			errs = errs.Append("HotelIDs", "contains an empty ID")
			break
		}
	}

	// A backwards or zero-length stay is worth catching locally: it is an easy
	// mistake and Amadeus reports it only as a generic invalid-date error.
	if !q.Stay.CheckIn.IsZero() && !q.Stay.CheckOut.IsZero() {
		if !q.Stay.CheckOut.After(q.Stay.CheckIn) {
			errs = append(errs, apierr.Invalidf("Stay",
				"check-out (%s) must be after check-in (%s)", q.Stay.CheckOut, q.Stay.CheckIn))
		}
	}
	if q.Stay.CheckIn.IsZero() && !q.Stay.CheckOut.IsZero() {
		errs = errs.Append("Stay.CheckIn", "is required when CheckOut is set")
	}

	if q.Guests.Adults < 0 || q.Guests.Adults > MaxAdults {
		errs = append(errs, apierr.Invalidf("Guests.Adults",
			"must be between 1 and %d, got %d", MaxAdults, q.Guests.Adults))
	}
	for _, age := range q.Guests.ChildAges {
		if age < 0 || age > MaxChildAge {
			errs = append(errs, apierr.Invalidf("Guests.ChildAges",
				"child ages must be between 0 and %d, got %d", MaxChildAge, age))
		}
	}
	if q.Rooms < 0 || q.Rooms > MaxRooms {
		errs = append(errs, apierr.Invalidf("Rooms",
			"must be between 1 and %d, got %d", MaxRooms, q.Rooms))
	}

	if q.BoardType != "" && !q.BoardType.IsValid() {
		errs = append(errs, apierr.Invalidf("BoardType", "%q is not a known board type", q.BoardType))
	}
	if q.PaymentPolicy != "" && !q.PaymentPolicy.IsValid() {
		errs = append(errs, apierr.Invalidf("PaymentPolicy", "%q is not a known payment policy", q.PaymentPolicy))
	}
	for _, code := range q.RateCodes {
		if !code.IsValid() {
			errs = append(errs, apierr.Invalidf("RateCodes",
				"%q is not a rate code; they are 3 uppercase alphanumeric characters", code))
		}
	}

	// Amadeus rejects a price range with no currency, since the numbers would
	// be meaningless.
	if q.PriceRange != "" && q.Currency == "" {
		errs = errs.Append("Currency", "is required when PriceRange is set")
	}

	return errs.OrNil()
}

func (q SearchQuery) params() url.Values {
	// hotelIds is the only parameter always sent. Everything else is emitted
	// only when set: Amadeus rejects empty and zero-valued parameters such as
	// adults=0 or currency=.
	values := url.Values{"hotelIds": {strings.Join(q.HotelIDs, ",")}}

	if !q.Stay.CheckIn.IsZero() {
		values.Set("checkInDate", q.Stay.CheckIn.String())
	}
	if !q.Stay.CheckOut.IsZero() {
		values.Set("checkOutDate", q.Stay.CheckOut.String())
	}
	if q.Guests.Adults > 0 {
		values.Set("adults", strconv.Itoa(q.Guests.Adults))
	}
	if len(q.Guests.ChildAges) > 0 {
		ages := make([]string, len(q.Guests.ChildAges))
		for i, age := range q.Guests.ChildAges {
			ages[i] = strconv.Itoa(age)
		}
		values.Set("childAges", strings.Join(ages, ","))
	}
	if q.Rooms > 0 {
		values.Set("roomQuantity", strconv.Itoa(q.Rooms))
	}
	if q.BoardType != "" {
		values.Set("boardType", string(q.BoardType))
	}
	if len(q.RateCodes) > 0 {
		values.Set("rateCodes", codes.Join(q.RateCodes))
	}
	if q.PaymentPolicy != "" {
		values.Set("paymentPolicy", string(q.PaymentPolicy))
	}
	if q.Currency != "" {
		values.Set("currency", q.Currency)
	}
	if q.PriceRange != "" {
		values.Set("priceRange", q.PriceRange)
	}
	if q.CountryOfResidence != "" {
		values.Set("countryOfResidence", q.CountryOfResidence)
	}
	if q.BestRateOnly != nil {
		values.Set("bestRateOnly", strconv.FormatBool(*q.BestRateOnly))
	}
	if q.IncludeClosed != nil {
		values.Set("includeClosed", strconv.FormatBool(*q.IncludeClosed))
	}
	if q.Lang != "" {
		values.Set("lang", q.Lang)
	}
	if q.PageOffset != "" {
		values.Set("page[offset]", q.PageOffset)
	}

	return values
}

// GetQuery fetches a single offer by its ID.
type GetQuery struct {
	// OfferID is the offer to retrieve. Required. Offer IDs expire, so one
	// held from an earlier session will fail here.
	OfferID OfferID
	// Lang requests descriptive text in a language, e.g. "FR".
	Lang string
}

func (q GetQuery) validate() error {
	var errs apierr.ValidationErrors
	if strings.TrimSpace(string(q.OfferID)) == "" {
		errs = errs.Append("OfferID", "is required")
	}
	return errs.OrNil()
}

func (q GetQuery) params() url.Values {
	values := url.Values{}
	if q.Lang != "" {
		values.Set("lang", q.Lang)
	}
	return values
}

// NewStay returns a Stay from two dates, and is a convenience over building the
// struct when the dates come from strings.
func NewStay(checkIn, checkOut datetime.Date) Stay {
	return Stay{CheckIn: checkIn, CheckOut: checkOut}
}
