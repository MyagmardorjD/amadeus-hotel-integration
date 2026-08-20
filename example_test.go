package sdk_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	sdk "github.com/techpartners-asia/amadeus-hotel-integration/v2"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/booking"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/content"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/datetime"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/geo"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/inventory"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/offers"
)

// These examples are the README's code, compiled. A README that has drifted
// from the API is worse than no README, and the only way to keep it honest is
// to make the compiler check it. They are not run - every one would need live
// credentials - but they must build.

func ExampleNew() {
	client, err := sdk.New(sdk.Config{
		ClientID:     os.Getenv("AMADEUS_CLIENT_ID"),
		ClientSecret: os.Getenv("AMADEUS_CLIENT_SECRET"),
		Environment:  sdk.Test,
	})
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	hotels, err := client.Inventory.ByCity(ctx, inventory.CityQuery{CityCode: "PAR"})
	if err != nil {
		return
	}
	fmt.Println(len(hotels))
}

// The quick-start flow: find hotels, price them, show the cheapest rate.
func Example_quickStart() {
	client, err := sdk.New(sdk.Config{
		ClientID:     os.Getenv("AMADEUS_CLIENT_ID"),
		ClientSecret: os.Getenv("AMADEUS_CLIENT_SECRET"),
		Environment:  sdk.Test,
	})
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	hotels, err := client.Inventory.ByCity(ctx, inventory.CityQuery{
		CityCode: "PAR",
		Filters: inventory.Filters{
			Radius:  5,
			Ratings: []codes.Rating{codes.Rating4, codes.Rating5},
		},
	})
	if err != nil {
		return
	}

	ids := inventory.IDs(hotels)
	if len(ids) > 20 {
		ids = ids[:20]
	}

	checkIn := datetime.MustParseDate("2026-08-10")
	results, err := client.Offers.Search(ctx, offers.SearchQuery{
		HotelIDs: ids,
		Stay:     offers.Stay{CheckIn: checkIn, CheckOut: checkIn.AddDays(3)},
		Guests:   offers.Guests{Adults: 2},
	})
	if err != nil {
		return
	}

	for _, result := range results {
		offer, ok := result.Cheapest()
		if !ok {
			continue
		}
		refundable, certain := offer.Policies.IsRefundable()
		fmt.Printf("%-40s %10s  refundable=%v (certain=%v)\n",
			result.Hotel.Name, offer.Price.Total, refundable, certain)
	}
}

func ExampleClient_inventorySearches() {
	var client *sdk.Client
	ctx := context.Background()

	_, _ = client.Inventory.ByCity(ctx, inventory.CityQuery{
		CityCode: "PAR",
		Filters: inventory.Filters{
			Radius:     10,
			RadiusUnit: geo.Kilometers,
			ChainCodes: []string{"HL", "MC"},
			Amenities:  []codes.Amenity{codes.AmenitySwimmingPool},
			Ratings:    []codes.Rating{codes.Rating5},
			Source:     codes.HotelSourceDirectChain,
		},
	})

	_, _ = client.Inventory.ByGeocode(ctx, inventory.GeocodeQuery{
		Position: geo.Coordinates{Latitude: 48.8566, Longitude: 2.3522},
		Filters:  inventory.Filters{Radius: 5},
	})

	_, _ = client.Inventory.ByIDs(ctx, inventory.IDsQuery{
		HotelIDs: []string{"MCLONGHM", "ACPAR419"},
	})

	suggestions, _ := client.Inventory.ByKeyword(ctx, inventory.KeywordQuery{
		Keyword:     "PARI",
		CountryCode: "FR",
	})
	for _, s := range suggestions {
		fmt.Println(s.Name, s.HotelIDs, s.Relevance)
	}
}

func ExampleClient_offerPricing() {
	var result offers.HotelOffers
	offer := result.Offers[0]

	_ = offer.Price.Total
	_ = offer.Price.Base
	_ = offer.Price.SellingTotal

	taxes, _ := offer.Price.TaxesTotal()
	onArrival, _ := offer.Price.PayableAtProperty()
	perNight, remainder, ok := offer.Price.PerNight(offer.Stay.Nights())

	fmt.Println(taxes, onArrival, perNight, remainder, ok)

	refundable, certain := offer.Policies.IsRefundable()
	if !certain {
		return
	}
	if deadline, ok := offer.Policies.FreeCancellationUntil(); ok {
		fmt.Println("Free cancellation until", deadline, refundable)
	}

	for _, room := range result.GroupByRoom() {
		fmt.Printf("%s - from %s (%d rates)\n",
			room.Room.Description, room.PriceFrom, len(room.Offers))
	}
}

func ExampleClient_contentLookup() {
	var client *sdk.Client
	ctx := context.Background()

	hotel, err := client.Content.Get(ctx, content.Query{
		HotelID: "HLPAR266",
		Lang:    "FR",
	})
	if err != nil {
		return
	}

	if position, ok := hotel.Position(); ok {
		fmt.Println(position)
	}
	if photo, ok := hotel.PrimaryPhoto(); ok {
		fmt.Println(photo.Best(400), photo.Alt)
	}
	fmt.Println(len(hotel.Rooms), hotel.Facilities, hotel.Policies,
		hotel.Awards, hotel.PointsOfInterest, hotel.NearbyLandmarks)
}

func ExampleClient_booking() {
	var client *sdk.Client
	var offer offers.Offer
	ctx := context.Background()

	order, err := client.Booking.Create(ctx, booking.Reservation{
		Guests: []booking.Guest{{
			ID:        1,
			Title:     "MS",
			FirstName: "Ada",
			LastName:  "Lovelace",
			Email:     "ada@example.com",
			Phone:     "+33679278416",
		}},
		Rooms: []booking.RoomRequest{{
			OfferID:        offer.ID.String(),
			GuestIDs:       []int{1},
			SpecialRequest: "High floor if possible",
		}},
		Payment: booking.Payment{
			Method: booking.PaymentCreditCard,
			Card: &booking.Card{
				VendorCode: "VI",
				Number:     "4111111111111111",
				Expiry:     "1230",
				HolderName: "ADA LOVELACE",
			},
		},
		Agent: booking.Agent{Email: "agency@example.com"},
	})
	if err != nil {
		return
	}

	b := order.Bookings[0]
	fmt.Println(b.Status.IsActive(), b.Status.IsCancelled())
	if number, ok := b.ConfirmationNumber(); ok {
		fmt.Println(number)
	}

	free, certain := b.Offer.Policies.CanCancelFreeOfCharge(time.Now())
	fmt.Println(free, certain)

	_, _ = client.Booking.Get(ctx, order.ID)
	_, _ = client.Booking.GetByReference(ctx, "JKL789")
	_, _ = client.Booking.Cancel(ctx, order.ID, b.ID)
	_, _ = client.Booking.Delete(ctx, order.ID, b.ID)

	newStay := booking.Stay{
		CheckIn:  datetime.MustParseDate("2026-08-11"),
		CheckOut: datetime.MustParseDate("2026-08-14"),
	}
	_, _ = client.Booking.Modify(ctx, order.ID, b.ID, booking.Modification{Stay: &newStay})
}

func ExampleClient_errorHandling() {
	var client *sdk.Client
	ctx := context.Background()

	_, err := client.Inventory.ByCity(ctx, inventory.CityQuery{CityCode: "PAR"})

	if errors.Is(err, sdk.ErrNotFound) {
		return
	}
	if errors.Is(err, sdk.ErrRateLimited) {
		return
	}
	if errors.Is(err, sdk.ErrServer) {
		return
	}
	if errors.Is(err, sdk.ErrValidation) {
		return
	}

	var apiErr *sdk.APIError
	if errors.As(err, &apiErr) {
		for _, d := range apiErr.Details {
			fmt.Println(d.Code, d.Title, d.Detail, d.Source)
		}
	}

	var errs sdk.ValidationErrors
	if errors.As(err, &errs) {
		for _, e := range errs {
			fmt.Printf("%s: %s\n", e.Field, e.Reason)
		}
	}
}

func ExampleCatalog() {
	for _, a := range codes.AllAmenities() {
		fmt.Println(a, a.Label())
	}

	var client *sdk.Client
	fmt.Println(client.Codes.Amenities())
}

// stubOffers is the README's example of substituting a fake service.
type stubOffers struct{ offers.Service }

func (stubOffers) Search(context.Context, offers.SearchQuery) ([]offers.HotelOffers, error) {
	return []offers.HotelOffers{}, nil
}

func ExampleService_stub() {
	client := &sdk.Client{Offers: stubOffers{}}

	results, err := client.Offers.Search(context.Background(), offers.SearchQuery{
		HotelIDs: []string{"MCLONGHM"},
	})
	fmt.Println(len(results), err)
	// Output: 0 <nil>
}
