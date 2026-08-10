package booking

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/apierr"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeus"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeus/dto/bookingres"
)

// Endpoint paths on the Hotel Booking APIs.
const (
	ordersPath  = "/v2/booking/hotel-orders"
	byReference = ordersPath + "/by-reference"
)

// gdsOriginSystem is the originSystemCode the by-reference lookup requires. A
// PNR locator is a GDS record, and the Enterprise guide documents GDS as the
// value for every example of this call.
const gdsOriginSystem = "GDS"

// Service creates and manages reservations. Obtain one from the SDK client:
//
//	client, _ := sdk.New(cfg)
//	order, err := client.Booking.Create(ctx, reservation)
//
// It is an interface so callers can substitute a fake in their own tests -
// which matters more here than elsewhere, since the real thing spends money.
type Service interface {
	// Create books a reservation.
	//
	// Against sdk.Production this charges a real card and creates a real
	// reservation. Persist the returned Order.ID immediately: it is the only
	// handle to the booking, and without it neither retrieval nor cancellation
	// is possible.
	Create(ctx context.Context, reservation Reservation) (*Order, error)

	// Get retrieves an order by its ID.
	Get(ctx context.Context, id OrderID) (*Order, error)
	// GetByReference retrieves an order by its GDS record locator.
	GetByReference(ctx context.Context, reference string) (*Order, error)

	// Cancel cancels one booking within an order, leaving the order and any
	// other bookings in it intact. It returns the updated order, in which the
	// booking's status is CANCELLED.
	//
	// Cancelling may incur a fee: check Policies.CanCancelFreeOfCharge on the
	// booked offer before calling, and tell the guest what it will cost.
	Cancel(ctx context.Context, orderID OrderID, bookingID BookingID) (*Order, error)

	// Modify changes one booking within an order. Only the fields set on the
	// Modification are sent; everything else is left as booked.
	//
	// A modification can reprice the stay. Amadeus returns the updated order,
	// so compare the price before and after rather than assuming it held.
	Modify(ctx context.Context, orderID OrderID, bookingID BookingID, change Modification) (*Order, error)

	// Delete removes one booking from an order, returning the provider's
	// cancellation reference.
	Delete(ctx context.Context, orderID OrderID, bookingID BookingID) (*CancellationResult, error)
}

// Modification describes a change to an existing booking. Only the fields you
// set are sent, so a zero Modification changes nothing.
type Modification struct {
	// SpecialRequest replaces the room's special request. Set it to a pointer
	// to an empty string to clear one; leaving it nil leaves it unchanged.
	SpecialRequest *string
	// GuestIDs replaces the guests occupying the room.
	GuestIDs []int
	// LoyaltyIDs maps a guest ID to their hotel loyalty membership.
	LoyaltyIDs map[int]string

	// OfferID switches the booking to a different offer, which is how a rate
	// change is made.
	OfferID string
	// Stay changes the dates.
	Stay *Stay
	// Guests changes the occupancy the booking is priced for.
	Guests *Guests
	// RateCode changes the rate code.
	RateCode codes.RateCode

	// Card replaces the payment card.
	Card *Card
}

// IsEmpty reports whether the modification would change nothing, which is worth
// catching before sending a no-op PATCH.
func (m Modification) IsEmpty() bool {
	return m.SpecialRequest == nil && len(m.GuestIDs) == 0 &&
		m.OfferID == "" && m.Stay == nil && m.Guests == nil &&
		m.RateCode == "" && m.Card == nil
}

func (m Modification) validate() error {
	var errs apierr.ValidationErrors

	if m.IsEmpty() {
		errs = errs.Append("Modification", "changes nothing; set at least one field")
	}
	if m.Stay != nil && !m.Stay.CheckIn.IsZero() && !m.Stay.CheckOut.IsZero() {
		if !m.Stay.CheckOut.After(m.Stay.CheckIn) {
			errs = append(errs, apierr.Invalidf("Stay",
				"check-out (%s) must be after check-in (%s)", m.Stay.CheckOut, m.Stay.CheckIn))
		}
	}
	if m.Card != nil {
		errs = m.Card.validate(errs)
	}
	if m.RateCode != "" && !m.RateCode.IsValid() {
		errs = append(errs, apierr.Invalidf("RateCode",
			"%q is not a rate code; they are 3 uppercase alphanumeric characters", m.RateCode))
	}

	return errs.OrNil()
}

type service struct {
	client *amadeus.Client
}

// NewService returns the booking service backed by client.
func NewService(client *amadeus.Client) Service {
	return &service{client: client}
}

func (s *service) Create(ctx context.Context, reservation Reservation) (*Order, error) {
	if err := reservation.validate(); err != nil {
		return nil, err
	}

	envelope, err := amadeus.Do[bookingres.HotelOrder](ctx, s.client, amadeus.Request{
		Method: http.MethodPost,
		Path:   ordersPath,
		Body:   toRequest(reservation),
		// The booking endpoints reject a body sent as application/json.
		AmadeusJSON: true,
	})
	if err != nil {
		return nil, err
	}

	order := mapOrder(envelope.Data)
	return &order, nil
}

func (s *service) Get(ctx context.Context, id OrderID) (*Order, error) {
	if strings.TrimSpace(string(id)) == "" {
		return nil, apierr.Invalid("OrderID", "is required")
	}
	return s.fetch(ctx, ordersPath+"/"+string(id))
}

// GetByReference looks an order up by its GDS record locator.
//
// The locator travels as a query parameter beside originSystemCode, not as a
// path segment: Amadeus answers /by-reference/{locator} with 400 INVALID
// FORMAT. Unlike the other retrievals this one returns its order inside an
// array, and answers an unknown locator with 200 and an empty element rather
// than a 404.
func (s *service) GetByReference(ctx context.Context, reference string) (*Order, error) {
	if strings.TrimSpace(reference) == "" {
		return nil, apierr.Invalid("reference", "is required")
	}

	envelope, err := amadeus.Do[[]bookingres.HotelOrder](ctx, s.client, amadeus.Request{
		Path: byReference,
		Query: url.Values{
			"originSystemCode": {gdsOriginSystem},
			"reference":        {reference},
		},
	})
	if err != nil {
		return nil, err
	}

	// An unknown locator comes back as 200 with a single empty object beside a
	// NO RESULT FOUND warning. Reporting that as a not-found error is what
	// makes errors.Is(err, sdk.ErrNotFound) work here as it does elsewhere.
	if len(envelope.Data) == 0 || envelope.Data[0].Id == "" {
		return nil, apierr.New(http.StatusNotFound, nil,
			fmt.Sprintf("no hotel order for reference %q", reference))
	}

	order := mapOrder(envelope.Data[0])
	return &order, nil
}

func (s *service) Cancel(ctx context.Context, orderID OrderID, bookingID BookingID) (*Order, error) {
	if err := validateIDs(orderID, bookingID); err != nil {
		return nil, err
	}

	envelope, err := amadeus.Do[bookingres.HotelOrder](ctx, s.client, amadeus.Request{
		Method: http.MethodPost,
		Path:   bookingPath(orderID, bookingID) + "/cancel",
	})
	if err != nil {
		return nil, err
	}

	order := mapOrder(envelope.Data)
	return &order, nil
}

func (s *service) Modify(ctx context.Context, orderID OrderID, bookingID BookingID, change Modification) (*Order, error) {
	if err := validateIDs(orderID, bookingID); err != nil {
		return nil, err
	}
	if err := change.validate(); err != nil {
		return nil, err
	}

	// Modify is the one endpoint whose updated order arrives under "included"
	// rather than "data"; "data" holds only a {type,id} reference.
	envelope, err := amadeus.Do[bookingres.UpdatedOrderReference](ctx, s.client, amadeus.Request{
		Method:      http.MethodPatch,
		Path:        bookingPath(orderID, bookingID),
		Body:        toUpdateRequest(change),
		AmadeusJSON: true,
	})
	if err != nil {
		return nil, err
	}

	included, err := decodeIncluded[bookingres.HotelOrder](envelope.Included)
	if err != nil {
		return nil, fmt.Errorf("booking: decoding the updated order: %w", err)
	}

	order := mapOrder(included)
	return &order, nil
}

func (s *service) Delete(ctx context.Context, orderID OrderID, bookingID BookingID) (*CancellationResult, error) {
	if err := validateIDs(orderID, bookingID); err != nil {
		return nil, err
	}

	// Delete returns its result under "included" too, with no "data" at all.
	envelope, err := amadeus.Do[struct{}](ctx, s.client, amadeus.Request{
		Method: http.MethodDelete,
		Path:   bookingPath(orderID, bookingID),
	})
	if err != nil {
		return nil, err
	}

	result, err := decodeIncluded[bookingres.DeleteBookingResult](envelope.Included)
	if err != nil {
		return nil, fmt.Errorf("booking: decoding the cancellation result: %w", err)
	}

	// Amadeus writes "NONE" when the provider returned no reference. That is a
	// successful cancellation without a number, not a failure, so it is
	// normalised to empty rather than passed through as a fake reference.
	number := result.CancellationNumber
	if number == noNumber {
		number = ""
	}
	return &CancellationResult{CancellationNumber: number}, nil
}

func (s *service) fetch(ctx context.Context, path string) (*Order, error) {
	envelope, err := amadeus.Do[bookingres.HotelOrder](ctx, s.client, amadeus.Request{Path: path})
	if err != nil {
		return nil, err
	}

	order := mapOrder(envelope.Data)
	return &order, nil
}

// bookingPath builds the path addressing one booking within an order.
func bookingPath(orderID OrderID, bookingID BookingID) string {
	return ordersPath + "/" + string(orderID) + "/hotel-bookings/" + string(bookingID)
}

func validateIDs(orderID OrderID, bookingID BookingID) error {
	var errs apierr.ValidationErrors
	if strings.TrimSpace(string(orderID)) == "" {
		errs = errs.Append("orderID", "is required")
	}
	if strings.TrimSpace(string(bookingID)) == "" {
		errs = errs.Append("bookingID", "is required")
	}
	return errs.OrNil()
}

// decodeIncluded decodes the "included" member of a response envelope, which
// the manage endpoints use instead of "data".
func decodeIncluded[T any](raw json.RawMessage) (T, error) {
	var out T
	if len(raw) == 0 {
		return out, fmt.Errorf("response carried no \"included\" member")
	}
	err := json.Unmarshal(raw, &out)
	return out, err
}
