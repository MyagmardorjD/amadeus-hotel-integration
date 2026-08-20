package offers

import (
	"context"
	"fmt"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/apierr"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeus"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeus/dto/offersdto"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/money"
)

// basePath is the Hotel Search (v3.5) endpoint root.
const basePath = "/v3/shopping/hotel-offers"

// Service prices stays. Obtain one from the SDK client:
//
//	client, _ := sdk.New(cfg)
//	results, err := client.Offers.Search(ctx, offers.SearchQuery{...})
//
// It is an interface so callers can substitute a fake in their own tests.
type Service interface {
	// Search prices a stay across a set of properties.
	Search(ctx context.Context, query SearchQuery) ([]HotelOffers, error)
	// Get retrieves a single offer by ID, and is how you re-verify a price
	// immediately before booking. Offer IDs expire; a stale one returns
	// apierr.ErrNotFound.
	Get(ctx context.Context, query GetQuery) (*OfferDetail, error)
}

// OfferDetail is a single offer together with the property it applies to.
//
// Get returns the hotel as well as the offer because Amadeus sends both, and a
// price without the property it belongs to cannot be displayed or checked.
type OfferDetail struct {
	// Hotel is the property the offer books.
	Hotel Hotel
	// Available reports whether the property still has inventory.
	Available bool
	// Offer is the bookable rate.
	Offer Offer

	// Rates are the currency conversions this response returned, present when
	// the search that produced the offer asked for a currency the hotel does
	// not quote in.
	//
	// The by-ID endpoint takes no currency parameter, yet Amadeus attaches the
	// rate regardless: the conversion context follows the offer ID. Keeping it
	// here is what lets the re-verify step show the same currency the search
	// showed, which matters because this is the call made immediately before
	// booking - the last point at which the price on the screen is settled.
	Rates ConversionRates
}

// Display converts an amount from this offer into the currency the original
// search asked for, ready to show to a user.
//
// It behaves exactly as HotelOffers.Display: the offer's own price is never
// mutated, since that is the currency the booking is charged in, and an amount
// no rate covers is returned unchanged with Converted false.
func (d OfferDetail) Display(amount money.Money) DisplayMoney {
	converted, ok := d.Rates.ConvertOrOriginal(amount)
	return DisplayMoney{
		Amount:    converted,
		Original:  amount,
		Converted: ok,
	}
}

// DisplayTotal is the amount to show the guest for this offer, converted into
// the requested currency.
//
// It takes no argument where HotelOffers.DisplayTotal takes an offer, because
// an OfferDetail already holds the single offer it describes. Like that method
// it displays Price.Payable() rather than Total, so an agency markup is
// included when one applies.
func (d OfferDetail) DisplayTotal() DisplayMoney {
	return d.Display(d.Offer.Price.Payable())
}

type service struct {
	client *amadeus.Client
}

// NewService returns the offers service backed by client.
func NewService(client *amadeus.Client) Service {
	return &service{client: client}
}

func (s *service) Search(ctx context.Context, query SearchQuery) ([]HotelOffers, error) {
	if err := query.validate(); err != nil {
		return nil, err
	}

	envelope, err := amadeus.Do[[]offersdto.OffersResponse](ctx, s.client, amadeus.Request{
		Path:  basePath,
		Query: query.params(),
	})
	if err != nil {
		return nil, err
	}
	return mapHotelOffers(envelope.Data, mapConversionRates(
		envelope.Dictionaries.CurrencyConversionLookupRates)), nil
}

func (s *service) Get(ctx context.Context, query GetQuery) (*OfferDetail, error) {
	if err := query.validate(); err != nil {
		return nil, err
	}

	// The by-ID endpoint returns the offer wrapped in its hotel, since a price
	// is meaningless without knowing the property it applies to.
	envelope, err := amadeus.Do[offersdto.OffersResponse](ctx, s.client, amadeus.Request{
		Path:  basePath + "/" + string(query.OfferID),
		Query: query.params(),
	})
	if err != nil {
		return nil, err
	}

	mapped := mapHotelOffers([]offersdto.OffersResponse{envelope.Data},
		mapConversionRates(envelope.Dictionaries.CurrencyConversionLookupRates))
	if len(mapped) == 0 || len(mapped[0].Offers) == 0 {
		// A 200 carrying no offer means the ID no longer resolves, which is
		// what an expired offer looks like. Reporting it as not-found beats
		// returning a nil offer with a nil error for the caller to trip over.
		return nil, fmt.Errorf("offers: %s returned no offer (it may have expired): %w",
			query.OfferID, apierr.ErrNotFound)
	}

	return &OfferDetail{
		Hotel:     mapped[0].Hotel,
		Available: mapped[0].Available,
		Offer:     mapped[0].Offers[0],
		Rates:     mapped[0].Rates,
	}, nil
}
