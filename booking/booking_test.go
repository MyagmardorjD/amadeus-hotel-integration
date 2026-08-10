package booking_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/apierr"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/booking"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/datetime"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeustest"
)

const ordersPath = "/v2/booking/hotel-orders"

// validReservation is a reservation that passes validation, so tests of a
// single invalid field change exactly one thing.
func validReservation() booking.Reservation {
	return booking.Reservation{
		Guests: []booking.Guest{{
			ID: 1, Title: "MS", FirstName: "Ada", LastName: "Lovelace",
			Email: "ada@example.invalid", Phone: "+33679278416",
		}},
		Rooms: []booking.RoomRequest{{
			OfferID:  "OFFERDELUXEFLEX",
			GuestIDs: []int{1},
		}},
		Payment: booking.Payment{
			Method: booking.PaymentCreditCard,
			Card: &booking.Card{
				VendorCode: "VI",
				Number:     "4111111111111111", // a documented Visa test number
				Expiry:     "1230",
				HolderName: "ADA LOVELACE",
			},
		},
		Agent: booking.Agent{Email: "agency@example.invalid"},
	}
}

func newService(t *testing.T) (booking.Service, *amadeustest.Server) {
	t.Helper()
	server := amadeustest.New(t)
	server.Fixture(t, http.MethodPost, ordersPath, "order")
	return booking.NewService(server.Client()), server
}

func TestCreateMapsTheOrder(t *testing.T) {
	service, _ := newService(t)

	order, err := service.Create(context.Background(), validReservation())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if order.ID != "XN_5FGHIJKLMN" {
		t.Errorf("ID = %q", order.ID)
	}
	if reference, ok := order.Reference(); !ok || reference != "JKL789" {
		t.Errorf("Reference() = %q, %v", reference, ok)
	}
	if !order.IsConfirmed() {
		t.Error("an order whose only booking is CONFIRMED should report IsConfirmed")
	}
	if len(order.Bookings) != 1 || len(order.Guests) != 2 {
		t.Fatalf("got %d bookings and %d guests", len(order.Bookings), len(order.Guests))
	}
}

func TestBookingDetailsAreMapped(t *testing.T) {
	service, _ := newService(t)
	order, err := service.Create(context.Background(), validReservation())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	b := order.Bookings[0]
	if b.ID != "BK_001" || b.Status != booking.StatusConfirmed {
		t.Errorf("booking = %s / %s", b.ID, b.Status)
	}
	if !b.Status.IsActive() {
		t.Error("CONFIRMED should be an active status")
	}
	if b.Hotel.ID != "HLPAR266" || b.Hotel.Name != "HILTON PARIS OPERA" {
		t.Errorf("hotel = %+v", b.Hotel)
	}
	if b.TravelAgentID != "00000000" {
		t.Errorf("TravelAgentID = %q", b.TravelAgentID)
	}

	if len(b.Rooms) != 1 {
		t.Fatalf("got %d rooms", len(b.Rooms))
	}
	room := b.Rooms[0]
	if room.SpecialRequest != "High floor if possible" {
		t.Errorf("SpecialRequest = %q", room.SpecialRequest)
	}
	if len(room.Guests) != 2 || room.Guests[0].HotelLoyaltyID != "HH123456789" {
		t.Errorf("room guests = %+v", room.Guests)
	}
}

func TestConfirmationNumberSkipsAmadeusPlaceholders(t *testing.T) {
	// Amadeus writes "......" for a confirmation not yet issued and "NONE" for
	// an absent cancellation reference. Passing either through as a real
	// reference would have a guest quote it at the desk.
	service, _ := newService(t)
	order, _ := service.Create(context.Background(), validReservation())

	b := order.Bookings[0]
	if number, ok := b.ConfirmationNumber(); !ok || number != "89124357" {
		t.Errorf("ConfirmationNumber() = %q, %v", number, ok)
	}
	if number, ok := b.CancellationNumber(); ok {
		t.Errorf(`CancellationNumber() = %q, want none: the fixture holds "NONE"`, number)
	}
}

func TestPendingIsNotAnActiveReservation(t *testing.T) {
	// An on-request booking the hotel has not accepted is not a room. Treating
	// it as one sends a guest to a property with no reservation.
	if booking.StatusPending.IsActive() {
		t.Error("PENDING must not report as active")
	}
	for _, status := range []booking.Status{booking.StatusConfirmed, booking.StatusOnHold, booking.StatusPast} {
		if !status.IsActive() {
			t.Errorf("%s should be active", status)
		}
	}
	for _, status := range []booking.Status{booking.StatusCancelled, booking.StatusDenied, booking.StatusDeleted} {
		if !status.IsCancelled() {
			t.Errorf("%s should be cancelled", status)
		}
	}
}

func TestBookedPriceIsMoney(t *testing.T) {
	service, _ := newService(t)
	order, _ := service.Create(context.Background(), validReservation())

	price := order.Bookings[0].Offer.Price
	if price == nil {
		t.Fatal("price was dropped")
	}
	if got := price.Total.String(); got != "600 EUR" {
		t.Errorf("Total = %q", got)
	}
	if got := price.SellingTotal.String(); got != "612 EUR" {
		t.Errorf("SellingTotal = %q", got)
	}
	if len(price.Taxes) != 2 {
		t.Fatalf("got %d taxes", len(price.Taxes))
	}

	// Only the non-included tax counts toward what is still owed.
	total, err := price.TaxesTotal()
	if err != nil {
		t.Fatalf("TaxesTotal: %v", err)
	}
	if total.String() != "12 EUR" {
		t.Errorf("TaxesTotal = %q, want 12 EUR", total)
	}
	if price.Taxes[1].Applicable == nil || price.Taxes[1].Applicable.Start.String() != "2026-08-10" {
		t.Errorf("tax date range = %+v", price.Taxes[1].Applicable)
	}
	if price.Variations == nil || len(price.Variations.Changes) != 2 {
		t.Errorf("variations = %+v", price.Variations)
	}
}

func TestCancellationDeadlineDrivesFreeCancellation(t *testing.T) {
	service, _ := newService(t)
	order, _ := service.Create(context.Background(), validReservation())

	policies := order.Bookings[0].Offer.Policies
	if policies == nil {
		t.Fatal("policies were dropped")
	}

	// Before the deadline it is free; after it, it is not. A method that only
	// checked whether a deadline exists would answer "free" in both cases.
	before := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	free, certain := policies.CanCancelFreeOfCharge(before)
	if !free || !certain {
		t.Errorf("before the deadline: free=%v certain=%v, want true/true", free, certain)
	}

	after := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	free, certain = policies.CanCancelFreeOfCharge(after)
	if free || !certain {
		t.Errorf("after the deadline: free=%v certain=%v, want false/true", free, certain)
	}
}

func TestUnknownPolicyReportsUncertainty(t *testing.T) {
	// With nothing to go on, the answer must be "I don't know", not "yes".
	var policies booking.Policies
	free, certain := policies.CanCancelFreeOfCharge(time.Now())
	if free || certain {
		t.Errorf("free=%v certain=%v, want false/false for an empty policy", free, certain)
	}
}

func TestCardIsReturnedMasked(t *testing.T) {
	service, _ := newService(t)
	order, _ := service.Create(context.Background(), validReservation())

	payment := order.Bookings[0].Payment
	if payment == nil || payment.Card == nil {
		t.Fatal("payment was dropped")
	}
	if payment.Method != booking.PaymentCreditCard {
		t.Errorf("Method = %q", payment.Method)
	}
	if !strings.HasPrefix(payment.Card.MaskedNumber, "XXXX") {
		t.Errorf("card number = %q, expected it masked as Amadeus sent it", payment.Card.MaskedNumber)
	}
	if payment.IATANumber != "00000000" {
		t.Errorf("IATANumber = %q", payment.IATANumber)
	}
}

func TestArrivalFlightIsMapped(t *testing.T) {
	service, _ := newService(t)
	order, _ := service.Create(context.Background(), validReservation())

	arrival := order.Bookings[0].Arrival
	if arrival == nil {
		t.Fatal("arrival information was dropped")
	}
	if arrival.CarrierCode != "AF" || arrival.FlightNumber != "1680" {
		t.Errorf("flight = %s%s", arrival.CarrierCode, arrival.FlightNumber)
	}
	if arrival.DepartureAirport != "LHR" || arrival.ArrivalAirport != "CDG" || arrival.Terminal != "2E" {
		t.Errorf("route = %+v", arrival)
	}
	if arrival.ArrivingAt == nil {
		t.Error("arrival time was dropped")
	}
}

func TestGuestsCarryBothIDs(t *testing.T) {
	// The temp ID is how a returned guest is matched back to the one that was
	// sent; the Amadeus ID is how it is referenced afterwards.
	service, _ := newService(t)
	order, _ := service.Create(context.Background(), validReservation())

	guest := order.Guests[0]
	if guest.ID != 2001 || guest.TempID != 1 {
		t.Errorf("guest IDs = %d / %d", guest.ID, guest.TempID)
	}
	if guest.FullName() != "ADA LOVELACE" {
		t.Errorf("FullName = %q", guest.FullName())
	}
	if len(guest.FrequentTraveler) != 1 || guest.FrequentTraveler[0].AirlineCode != "AF" {
		t.Errorf("frequent traveler = %+v", guest.FrequentTraveler)
	}
	if order.Guests[1].ChildAge != 8 {
		t.Errorf("child age = %d", order.Guests[1].ChildAge)
	}
}

func TestCreateSendsTheAmadeusContentType(t *testing.T) {
	// The booking endpoints reject a body sent as application/json.
	service, server := newService(t)
	if _, err := service.Create(context.Background(), validReservation()); err != nil {
		t.Fatalf("Create: %v", err)
	}

	request := server.LastRequest(t)
	if got := request.Header.Get("Content-Type"); got != "application/vnd.amadeus+json" {
		t.Errorf("Content-Type = %q", got)
	}
}

func TestCreateBodyCarriesEveryField(t *testing.T) {
	// A field dropped on the way out is a booking made on terms the caller did
	// not choose, so the outbound body is asserted field by field.
	service, server := newService(t)

	childAge := 8
	reservation := validReservation()
	reservation.Guests = append(reservation.Guests, booking.Guest{
		ID: 2, Title: "CHILD", FirstName: "Byron", LastName: "Lovelace", ChildAge: &childAge,
	})
	reservation.Rooms[0].GuestIDs = []int{1, 2}
	reservation.Rooms[0].LoyaltyIDs = map[int]string{1: "HH123456789"}
	reservation.Rooms[0].SpecialRequest = "High floor if possible"
	reservation.Payment.Instructions = "Charge on arrival"
	reservation.Agent.ID = "00000000"

	if _, err := service.Create(context.Background(), reservation); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var sent struct {
		Data struct {
			Type   string `json:"type"`
			Guests []struct {
				Tid       int    `json:"tid"`
				FirstName string `json:"firstName"`
				ChildAge  *int   `json:"childAge"`
			} `json:"guests"`
			RoomAssociations []struct {
				HotelOfferID    string `json:"hotelOfferId"`
				SpecialRequest  string `json:"specialRequest"`
				GuestReferences []struct {
					GuestReference string `json:"guestReference"`
					HotelLoyaltyID string `json:"hotelLoyaltyId"`
				} `json:"guestReferences"`
			} `json:"roomAssociations"`
			Payment struct {
				Method              string `json:"method"`
				PaymentInstructions string `json:"paymentInstructions"`
				PaymentCard         struct {
					PaymentCardInfo struct {
						VendorCode string `json:"vendorCode"`
						CardNumber string `json:"cardNumber"`
						ExpiryDate string `json:"expiryDate"`
					} `json:"paymentCardInfo"`
				} `json:"paymentCard"`
			} `json:"payment"`
			TravelAgent struct {
				TravelAgentID string `json:"travelAgentId"`
				Contact       struct {
					Email string `json:"email"`
				} `json:"contact"`
			} `json:"travelAgent"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(server.LastRequest(t).Body), &sent); err != nil {
		t.Fatalf("decoding the sent body: %v", err)
	}

	if sent.Data.Type != "hotel-order" {
		t.Errorf("type = %q", sent.Data.Type)
	}
	if len(sent.Data.Guests) != 2 {
		t.Fatalf("sent %d guests, want 2", len(sent.Data.Guests))
	}
	// tid must survive even for a guest numbered 0, which is why the wire
	// struct does not mark it omitempty.
	if sent.Data.Guests[0].Tid != 1 {
		t.Errorf("guest tid = %d", sent.Data.Guests[0].Tid)
	}
	if sent.Data.Guests[1].ChildAge == nil || *sent.Data.Guests[1].ChildAge != 8 {
		t.Errorf("child age was dropped: %+v", sent.Data.Guests[1].ChildAge)
	}

	room := sent.Data.RoomAssociations[0]
	if room.HotelOfferID != "OFFERDELUXEFLEX" || room.SpecialRequest != "High floor if possible" {
		t.Errorf("room = %+v", room)
	}
	if len(room.GuestReferences) != 2 {
		t.Fatalf("sent %d guest references", len(room.GuestReferences))
	}
	if room.GuestReferences[0].GuestReference != "1" || room.GuestReferences[0].HotelLoyaltyID != "HH123456789" {
		t.Errorf("guest reference = %+v", room.GuestReferences[0])
	}

	if sent.Data.Payment.Method != "CREDIT_CARD" {
		t.Errorf("payment method = %q", sent.Data.Payment.Method)
	}
	card := sent.Data.Payment.PaymentCard.PaymentCardInfo
	if card.VendorCode != "VI" || card.CardNumber != "4111111111111111" || card.ExpiryDate != "1230" {
		t.Errorf("card = %+v", card)
	}
	if sent.Data.TravelAgent.Contact.Email != "agency@example.invalid" {
		t.Errorf("agent email = %q", sent.Data.TravelAgent.Contact.Email)
	}
}

// Validation is stricter here than elsewhere in the SDK, because a malformed
// booking either fails after the guest was told it worked, or succeeds on terms
// nobody intended.
func TestReservationValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*booking.Reservation)
	}{
		{"no guests", func(r *booking.Reservation) { r.Guests = nil }},
		{"no rooms", func(r *booking.Reservation) { r.Rooms = nil }},
		{"duplicate guest IDs", func(r *booking.Reservation) {
			r.Guests = append(r.Guests, booking.Guest{ID: 1, FirstName: "Bob", LastName: "Smith"})
		}},
		{"room references an unknown guest", func(r *booking.Reservation) {
			r.Rooms[0].GuestIDs = []int{99}
		}},
		{"loyalty ID for an unknown guest", func(r *booking.Reservation) {
			r.Rooms[0].LoyaltyIDs = map[int]string{99: "HH1"}
		}},
		{"missing first name", func(r *booking.Reservation) { r.Guests[0].FirstName = "" }},
		{"accented name Amadeus rejects", func(r *booking.Reservation) { r.Guests[0].LastName = "Lovelacé" }},
		{"malformed guest email", func(r *booking.Reservation) { r.Guests[0].Email = "not-an-email" }},
		{"missing agent email", func(r *booking.Reservation) { r.Agent.Email = "" }},
		{"missing offer ID", func(r *booking.Reservation) { r.Rooms[0].OfferID = "" }},
		{"no guests in the room", func(r *booking.Reservation) { r.Rooms[0].GuestIDs = nil }},
		{"unknown payment method", func(r *booking.Reservation) { r.Payment.Method = "CASH" }},
		{"card payment with no card", func(r *booking.Reservation) { r.Payment.Card = nil }},
		{"mistyped card number", func(r *booking.Reservation) { r.Payment.Card.Number = "4111111111111112" }},
		{"card number too short", func(r *booking.Reservation) { r.Payment.Card.Number = "411111" }},
		{"malformed expiry", func(r *booking.Reservation) { r.Payment.Card.Expiry = "12/30" }},
		{"malformed CVV", func(r *booking.Reservation) { r.Payment.Card.SecurityCode = "12" }},
		{"bad vendor code", func(r *booking.Reservation) { r.Payment.Card.VendorCode = "VISA" }},
		{"child age out of range", func(r *booking.Reservation) {
			age := 40
			r.Guests[0].ChildAge = &age
		}},
		{"over-long special request", func(r *booking.Reservation) {
			r.Rooms[0].SpecialRequest = strings.Repeat("x", 200)
		}},
		{"too many rooms", func(r *booking.Reservation) {
			for range 10 {
				r.Rooms = append(r.Rooms, booking.RoomRequest{OfferID: "OFFERX", GuestIDs: []int{1}})
			}
		}},
	}

	service, server := newService(t)
	before := len(server.Requests())

	for _, c := range cases {
		reservation := validReservation()
		c.mutate(&reservation)

		if _, err := service.Create(context.Background(), reservation); !errors.Is(err, apierr.ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", c.name, err)
		}
	}

	if after := len(server.Requests()); after != before {
		t.Errorf("%d invalid reservations reached the network", after-before)
	}
}

func TestValidReservationPassesValidation(t *testing.T) {
	// Guards the negative cases above: if the baseline were itself invalid,
	// every one of them would pass for the wrong reason.
	service, _ := newService(t)
	if _, err := service.Create(context.Background(), validReservation()); err != nil {
		t.Fatalf("the baseline reservation should be valid, got: %v", err)
	}
}

func TestCardNumberNeverAppearsInAValidationError(t *testing.T) {
	// Validation errors get logged. A card number must not travel with them.
	reservation := validReservation()
	reservation.Payment.Card.Number = "4111111111111112" // fails Luhn

	service, _ := newService(t)
	_, err := service.Create(context.Background(), reservation)
	if err == nil {
		t.Fatal("expected a validation error")
	}
	if strings.Contains(err.Error(), "4111111111111112") {
		t.Errorf("the card number leaked into the error message: %v", err)
	}
}

func TestGetAndGetByReference(t *testing.T) {
	server := amadeustest.New(t)
	server.Fixture(t, http.MethodGet, ordersPath+"/XN_5FGHIJKLMN", "order")
	// by-reference takes the locator as a query parameter, and wraps its order
	// in an array, so it cannot share the by-ID fixture verbatim.
	server.JSON(http.MethodGet, ordersPath+"/by-reference", http.StatusOK,
		`{"data":[`+string(orderFixtureData(t))+`]}`)
	service := booking.NewService(server.Client())

	byID, err := service.Get(context.Background(), "XN_5FGHIJKLMN")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if byID.ID != "XN_5FGHIJKLMN" {
		t.Errorf("Get returned %q", byID.ID)
	}

	byRef, err := service.GetByReference(context.Background(), "JKL789")
	if err != nil {
		t.Fatalf("GetByReference: %v", err)
	}
	if byRef.ID != "XN_5FGHIJKLMN" {
		t.Errorf("GetByReference returned %q", byRef.ID)
	}
}

func TestCancelUpdatesTheBookingStatus(t *testing.T) {
	server := amadeustest.New(t)
	server.JSON(http.MethodPost, ordersPath+"/XN_5FGHIJKLMN/hotel-bookings/BK_001/cancel", http.StatusOK,
		`{"data":{"type":"hotel-order","id":"XN_5FGHIJKLMN","hotelBookings":[
		  {"id":"BK_001","bookingStatus":"CANCELLED","hotelProviderInformation":[
		    {"hotelProviderCode":"HL","confirmationNumber":"89124357","cancellationNumber":"CX9911"}]}]}}`)
	service := booking.NewService(server.Client())

	order, err := service.Cancel(context.Background(), "XN_5FGHIJKLMN", "BK_001")
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	b := order.Bookings[0]
	if b.Status != booking.StatusCancelled || !b.Status.IsCancelled() {
		t.Errorf("status = %q", b.Status)
	}
	if number, ok := b.CancellationNumber(); !ok || number != "CX9911" {
		t.Errorf("CancellationNumber() = %q, %v", number, ok)
	}
}

func TestModifyReadsTheUpdatedOrderFromIncluded(t *testing.T) {
	// Modify is the one endpoint whose result arrives under "included";
	// "data" holds only a {type,id} reference. Reading "data" would return an
	// order with no bookings and no error.
	server := amadeustest.New(t)
	server.JSON(http.MethodPatch, ordersPath+"/XN_5FGHIJKLMN/hotel-bookings/BK_001", http.StatusOK,
		`{"data":{"type":"hotel-order","id":"XN_5FGHIJKLMN"},
		  "included":{"type":"hotel-order","id":"XN_5FGHIJKLMN","hotelBookings":[
		    {"id":"BK_001","bookingStatus":"CONFIRMED",
		     "hotelOffer":{"id":"OFFER2","checkInDate":"2026-08-11","checkOutDate":"2026-08-14",
		       "price":{"currency":"EUR","total":"640.00"}}}]}}`)
	service := booking.NewService(server.Client())

	newStay := booking.Stay{
		CheckIn:  datetime.MustParseDate("2026-08-11"),
		CheckOut: datetime.MustParseDate("2026-08-14"),
	}
	order, err := service.Modify(context.Background(), "XN_5FGHIJKLMN", "BK_001",
		booking.Modification{Stay: &newStay})
	if err != nil {
		t.Fatalf("Modify: %v", err)
	}

	if len(order.Bookings) != 1 {
		t.Fatalf("the updated order was not read from \"included\": %+v", order)
	}
	if got := order.Bookings[0].Offer.Price.Total.String(); got != "640 EUR" {
		t.Errorf("repriced total = %q, want 640 EUR", got)
	}
	if got := order.Bookings[0].Offer.Stay.CheckIn.String(); got != "2026-08-11" {
		t.Errorf("new check-in = %q", got)
	}
}

func TestModifyRejectsAnEmptyChange(t *testing.T) {
	service, server := newService(t)

	before := len(server.Requests())
	_, err := service.Modify(context.Background(), "ORDER", "BOOKING", booking.Modification{})
	if !errors.Is(err, apierr.ErrValidation) {
		t.Errorf("err = %v, want ErrValidation for a no-op modification", err)
	}
	if len(server.Requests()) != before {
		t.Error("a no-op modification was sent anyway")
	}
}

func TestDeleteReturnsTheCancellationReference(t *testing.T) {
	server := amadeustest.New(t)
	server.JSON(http.MethodDelete, ordersPath+"/ORD/hotel-bookings/BK", http.StatusOK,
		`{"included":{"cancellationNumber":"CX9911"}}`)
	service := booking.NewService(server.Client())

	result, err := service.Delete(context.Background(), "ORD", "BK")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if result.CancellationNumber != "CX9911" {
		t.Errorf("CancellationNumber = %q", result.CancellationNumber)
	}
}

func TestDeleteNormalisesTheNonePlaceholder(t *testing.T) {
	// "NONE" means the provider gave no reference. It is a successful
	// cancellation, and must not be handed back as if it were a reference.
	server := amadeustest.New(t)
	server.JSON(http.MethodDelete, ordersPath+"/ORD/hotel-bookings/BK", http.StatusOK,
		`{"included":{"cancellationNumber":"NONE"}}`)
	service := booking.NewService(server.Client())

	result, err := service.Delete(context.Background(), "ORD", "BK")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if result.CancellationNumber != "" {
		t.Errorf(`CancellationNumber = %q, want "" for the NONE placeholder`, result.CancellationNumber)
	}
}

func TestManageOperationsRequireIDs(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()

	if _, err := service.Get(ctx, ""); !errors.Is(err, apierr.ErrValidation) {
		t.Errorf("Get(\"\") = %v", err)
	}
	if _, err := service.GetByReference(ctx, ""); !errors.Is(err, apierr.ErrValidation) {
		t.Errorf("GetByReference(\"\") = %v", err)
	}
	if _, err := service.Cancel(ctx, "", "BK"); !errors.Is(err, apierr.ErrValidation) {
		t.Errorf("Cancel with no order ID = %v", err)
	}
	if _, err := service.Delete(ctx, "ORD", ""); !errors.Is(err, apierr.ErrValidation) {
		t.Errorf("Delete with no booking ID = %v", err)
	}
}

func TestAmadeusRejectionSurfacesTyped(t *testing.T) {
	server := amadeustest.New(t)
	server.JSON(http.MethodPost, ordersPath, http.StatusBadRequest,
		`{"errors":[{"status":400,"code":38189,"title":"INVALID DATA RECEIVED","detail":"offer no longer available","source":{"pointer":"/data/roomAssociations/0/hotelOfferId"}}]}`)
	service := booking.NewService(server.Client())

	_, err := service.Create(context.Background(), validReservation())
	if !errors.Is(err, apierr.ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}

	var apiErr *apierr.APIError
	if !errors.As(err, &apiErr) || len(apiErr.Details) != 1 {
		t.Fatalf("err = %v, want an APIError with one detail", err)
	}
	if apiErr.Details[0].Source.Pointer != "/data/roomAssociations/0/hotelOfferId" {
		t.Errorf("source = %+v, want the pointer to the offending field", apiErr.Details[0].Source)
	}
}

func TestBookedPricePayableFallsBackToTotal(t *testing.T) {
	// A booked order without a markup: Payable is Total, not the zero
	// SellingTotal.
	service, _ := newService(t)
	order, _ := service.Create(context.Background(), validReservation())

	price := order.Bookings[0].Offer.Price
	if price == nil {
		t.Fatal("price was dropped")
	}
	if price.HasMarkup() {
		if price.Payable().String() != price.SellingTotal.String() {
			t.Errorf("with a markup, Payable() should be SellingTotal")
		}
	} else if price.Payable().String() != price.Total.String() {
		t.Errorf("Payable() = %s, want Total %s", price.Payable(), price.Total)
	}
}

func TestGetByReferenceSendsQueryParametersNotAPathSegment(t *testing.T) {
	// The Enterprise guide documents this endpoint as
	//   GET /v2/booking/hotel-orders/by-reference?originSystemCode=GDS&reference=X
	// Appending the reference as a path segment instead returns 400 INVALID
	// FORMAT from Amadeus, so this is the difference between working and not.
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, "/v2/booking/hotel-orders/by-reference", http.StatusOK,
		`{"data":[{"id":"ORDER1","type":"hotel-order"}]}`)

	order, err := booking.NewService(server.Client()).GetByReference(context.Background(), "3HHCAJ")
	if err != nil {
		t.Fatalf("GetByReference: %v", err)
	}
	if order.ID != "ORDER1" {
		t.Errorf("order.ID = %q, want ORDER1", order.ID)
	}

	got := server.LastRequest(t)
	if got.Path != "/v2/booking/hotel-orders/by-reference" {
		t.Errorf("path = %q, want the bare by-reference path", got.Path)
	}
	if v := got.Query.Get("reference"); v != "3HHCAJ" {
		t.Errorf("query reference = %q, want 3HHCAJ", v)
	}
	if v := got.Query.Get("originSystemCode"); v != "GDS" {
		t.Errorf("query originSystemCode = %q, want GDS", v)
	}
}

func TestGetByReferenceReportsAnUnknownReferenceAsNotFound(t *testing.T) {
	// Amadeus answers an unknown locator with 200, a NO RESULT FOUND warning
	// and a data array holding one empty object. Returning that as a blank
	// Order would tell the caller a booking exists when none does.
	server := amadeustest.New(t)
	server.JSON(http.MethodGet, "/v2/booking/hotel-orders/by-reference", http.StatusOK,
		`{"warnings":[{"code":34629,"detail":"NO RESULT FOUND"}],"data":[{}]}`)

	_, err := booking.NewService(server.Client()).GetByReference(context.Background(), "NOSUCH")
	if !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// orderFixtureData returns the "data" member of the order fixture, so a test
// needing the array-wrapped shape by-reference returns can reuse the same
// captured order rather than duplicating it.
func orderFixtureData(t *testing.T) []byte {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(amadeustest.Load(t, "order"), &envelope); err != nil {
		t.Fatalf("decoding the order fixture: %v", err)
	}
	return envelope.Data
}

func TestAGuestCannotOccupyTwoRooms(t *testing.T) {
	// A person is in one room. Sending the same guest ID in two room
	// associations is rejected by Amadeus as "Ids must be unique", a 400 that
	// names neither the guest nor the room - so it is caught here instead,
	// which is the same reason the SDK checks that a guest ID exists at all.
	r := validReservation()
	r.Guests = []booking.Guest{
		{ID: 1, Title: "MS", FirstName: "Ada", LastName: "Lovelace",
			Email: "ada@example.invalid", Phone: "+33679278416"},
		{ID: 2, Title: "MR", FirstName: "Alan", LastName: "Turing",
			Email: "alan@example.invalid", Phone: "+33679278417"},
	}
	r.Rooms = []booking.RoomRequest{
		{OfferID: "OFFER1", GuestIDs: []int{1, 2}},
		{OfferID: "OFFER2", GuestIDs: []int{2}}, // guest 2 is already in room 0
	}

	service, _ := newService(t)
	_, err := service.Create(context.Background(), r)
	if !errors.Is(err, apierr.ErrValidation) {
		t.Fatalf("err = %v, want a validation error", err)
	}
	if !strings.Contains(err.Error(), "2") {
		t.Errorf("the error should name the repeated guest: %v", err)
	}

	// The same guest twice within one room is equally impossible.
	r.Rooms = []booking.RoomRequest{{OfferID: "OFFER1", GuestIDs: []int{1, 1}}}
	if _, err := service.Create(context.Background(), r); !errors.Is(err, apierr.ErrValidation) {
		t.Errorf("a guest repeated within one room was accepted: %v", err)
	}

	// One guest per room, no repeats, must still pass.
	r.Rooms = []booking.RoomRequest{
		{OfferID: "OFFER1", GuestIDs: []int{1}},
		{OfferID: "OFFER2", GuestIDs: []int{2}},
	}
	if _, err := service.Create(context.Background(), r); err != nil {
		t.Errorf("a valid two-room reservation was rejected: %v", err)
	}
}
