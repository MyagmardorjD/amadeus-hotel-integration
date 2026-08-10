package booking

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/apierr"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/money"
)

// Limits Amadeus enforces on a booking request.
const (
	// MaxRooms is the most rooms one order can hold. Multi-room orders must be
	// the same hotel, the same dates and the same supplier.
	MaxRooms = 9
	// MaxSpecialRequest is the longest special request the reception accepts.
	MaxSpecialRequest = 120
	// MaxNameLength is the combined limit Amadeus places on title, first name
	// and last name together.
	MaxNameLength = 62
)

// PaymentMethod is how a booking is paid for.
type PaymentMethod string

const (
	// PaymentCreditCard charges a card supplied with the booking.
	PaymentCreditCard PaymentMethod = "CREDIT_CARD"
	// PaymentAgencyAccount pays through the agency's credit line.
	PaymentAgencyAccount PaymentMethod = "AGENCY_ACCOUNT"
	// PaymentTravelAgentID pays against an IATA booking source.
	PaymentTravelAgentID PaymentMethod = "TRAVEL_AGENT_ID"
	// PaymentVCCBillback pays via a billback provider such as Conferma.
	PaymentVCCBillback PaymentMethod = "VCC_BILLBACK"
	// PaymentVCCB2BWallet pays with a virtual card between agency and Amadeus
	// Merchant.
	PaymentVCCB2BWallet PaymentMethod = "VCC_B2B_WALLET"
	// PaymentCreditCardAgency uses the agency's card, for Amadeus Value Hotel.
	PaymentCreditCardAgency PaymentMethod = "CREDIT_CARD_AGENCY"
	// PaymentCreditCardTraveler uses the guest's card, for Amadeus Value Hotel.
	PaymentCreditCardTraveler PaymentMethod = "CREDIT_CARD_TRAVELER"
)

// AllPaymentMethods returns every payment method Amadeus accepts.
func AllPaymentMethods() []PaymentMethod {
	return []PaymentMethod{
		PaymentCreditCard, PaymentAgencyAccount, PaymentTravelAgentID,
		PaymentVCCBillback, PaymentVCCB2BWallet,
		PaymentCreditCardAgency, PaymentCreditCardTraveler,
	}
}

// IsValid reports whether m is a method Amadeus accepts.
func (m PaymentMethod) IsValid() bool {
	for _, valid := range AllPaymentMethods() {
		if m == valid {
			return true
		}
	}
	return false
}

// RequiresCard reports whether the method needs card details supplied.
func (m PaymentMethod) RequiresCard() bool {
	return m == PaymentCreditCard || m == PaymentCreditCardAgency || m == PaymentCreditCardTraveler
}

// Reservation is a request to book.
//
// Every field it needs is required, because a partially specified booking is
// not something the SDK can sensibly default: guessing a payment method or
// omitting an agent contact produces either a rejection or, worse, a booking
// made on terms nobody chose.
//
//	order, err := client.Booking.Create(ctx, booking.Reservation{
//	    Guests: []booking.Guest{{
//	        ID: 1, Title: "MR", FirstName: "Ada", LastName: "Lovelace",
//	        Email: "ada@example.com", Phone: "+33679278416",
//	    }},
//	    Rooms: []booking.RoomRequest{{
//	        OfferID: offer.ID.String(),
//	        GuestIDs: []int{1},
//	    }},
//	    Payment: booking.Payment{
//	        Method: booking.PaymentCreditCard,
//	        Card: &booking.Card{
//	            VendorCode: "VI", Number: "4111111111111111",
//	            Expiry: "1230", HolderName: "ADA LOVELACE",
//	        },
//	    },
//	    Agent: booking.Agent{Email: "agency@example.com"},
//	})
type Reservation struct {
	// Guests are everyone travelling. Each needs an ID unique within this
	// request, which Rooms reference. Required.
	Guests []Guest
	// Rooms are the rooms to book, one per offer. At least one, at most
	// MaxRooms. Required.
	Rooms []RoomRequest
	// Payment is how the booking is paid for. Required.
	Payment Payment
	// Agent identifies the booking travel agent. Its email is required.
	Agent Agent

	// Arrival is the guest's inbound flight, passed to the property.
	Arrival *ArrivalRequest
	// AddToPNR attaches this booking to an existing Amadeus PNR instead of
	// creating a new order.
	AddToPNR *PNRReference
}

// Guest is a person travelling on a booking.
type Guest struct {
	// ID is a caller-assigned identifier, unique within this reservation, that
	// Rooms reference. Any positive integer works; numbering from 1 is
	// conventional.
	ID int
	// Title is the guest's title: MR, MRS, MS, MISS, DR, CHILD, SIR, MADAM,
	// MESSRS.
	Title string
	// FirstName and LastName must be English letters and spaces only; Amadeus
	// rejects accents and punctuation. Both are required.
	FirstName string
	LastName  string
	// Phone is best given in E.123 form, e.g. "+33679278416".
	Phone string
	// Email is the guest's address.
	Email string
	// ChildAge marks the guest as a child, and must be set for one. Leave it
	// nil for an adult. It is a pointer because 0 is a meaningful age - an
	// infant under one - and must be distinguishable from "not set".
	ChildAge *int
	// FrequentTraveler is the guest's airline loyalty membership. Only the
	// first is passed to the hotel provider, so supply at most one.
	FrequentTraveler []FrequentTraveler
}

// IsChild reports whether the guest is travelling as a child.
func (g Guest) IsChild() bool { return g.ChildAge != nil }

// RoomRequest is one room to book.
type RoomRequest struct {
	// OfferID is the offer to book, from a search. Offer IDs expire, so
	// re-fetch or re-verify one held for more than a few minutes. Required.
	OfferID string
	// GuestIDs reference the guests occupying the room. The first is the main
	// guest, who holds the reservation and the form of payment. Required.
	GuestIDs []int
	// LoyaltyIDs maps a guest ID to their hotel chain rewards membership.
	// The chain rejects the booking if a number is invalid.
	LoyaltyIDs map[int]string
	// SpecialRequest is free text for the reception, up to MaxSpecialRequest
	// characters. It is a request, not a guarantee.
	SpecialRequest string
	// ManualMarkup overrides the markup Margin Manager would compute.
	ManualMarkup *money.Money
}

// Payment is how a booking is paid for.
type Payment struct {
	// Method is required.
	Method PaymentMethod
	// Card is required when Method.RequiresCard.
	Card *Card
	// Instructions is free text passed to the hotelier.
	Instructions string
	// PayerCode is the corporation code for VCC_B2B_WALLET generation.
	PayerCode string
	// IATANumber guarantees the booking. Taken from the Amadeus office profile
	// when omitted.
	IATANumber string
	// Supplier holds the hotel supplier's contact details.
	Supplier *SupplierContact
	// BillBack configures a VCC_BILLBACK payment.
	BillBack *BillBack
}

// Card is the payment card details.
//
// This is the only place the SDK handles a full card number. It is sent to
// Amadeus over TLS and is not retained, logged or included in any error the SDK
// produces.
type Card struct {
	// VendorCode is the two-letter card type: VI (Visa), CA (MasterCard),
	// AX (American Express). Required.
	VendorCode string
	// Number is the card number, 14 to 19 digits. Required.
	Number string
	// Expiry is the expiry date as "MMYY" or "YYYY-MM". Required.
	Expiry string
	// SecurityCode is the CVV/CVC. Strongly recommended, and required by many
	// aggregators, which reject the booking without it.
	SecurityCode string
	// HolderName is the cardholder's name.
	HolderName string
	// ThreeDS carries 3-D Secure authentication, which is mandatory for
	// European cards under PSD2.
	ThreeDS *ThreeDSecure
	// BillingAddress is the cardholder's address.
	BillingAddress *Address
}

// ThreeDSecure is a completed 3-D Secure authentication.
type ThreeDSecure struct {
	// Version is the protocol version, e.g. "2.2". Required.
	Version string
	// ECI is the Electronic Commerce Indicator. Required for versions 1.0.2
	// and 2.1.0.
	ECI string
	// CryptogramValue is the authentication cryptogram, base-64 encoded.
	// Required.
	CryptogramValue string
	// DSTransactionID is the directory server's transaction ID (3DS v2).
	DSTransactionID string
	// XID is the transaction ID for versions below 2.0.
	XID string
	// TransStatus is the outcome for v2, ParesStatus for v1.
	TransStatus string
	ParesStatus string
	VeresStatus string
}

// Address is a postal address.
type Address struct {
	Lines       []string
	PostalCode  string
	CityName    string
	PostalBox   string
	StateCode   string
	CountryCode string
}

// BillBack configures payment through a billback provider.
type BillBack struct {
	// ProviderCode is the provider, "CN" for Conferma. Required.
	ProviderCode string
	// AccountNumber is the provider account. Required.
	AccountNumber string
	// TravelAgencyID is the agency's provider account (CAI).
	TravelAgencyID string
	// BookerID is the agent's provider ID (CBI).
	BookerID string
}

// Agent identifies the booking travel agent.
type Agent struct {
	// Email is the agency address. Required: Amadeus rejects a booking without
	// one, since it is where confirmations go.
	Email string
	// Phone and Fax are taken from the Amadeus office profile when omitted.
	Phone string
	Fax   string
	// ID is the travel agent ID / IATA number receiving commission. It
	// defaults to the connected office profile's number.
	ID string
}

// ArrivalRequest is the guest's inbound flight.
type ArrivalRequest struct {
	// CarrierCode is the airline, e.g. "LH". Required.
	CarrierCode string
	// FlightNumber is the flight, e.g. "1050". Required.
	FlightNumber string
	// DepartureAirport is the origin IATA code. Required.
	DepartureAirport string
	// ArrivalAirport is the destination IATA code.
	ArrivalAirport string
	// Terminal is the arrival terminal.
	Terminal string
	// ArrivingAt is the local arrival time.
	ArrivingAt *time.Time
}

// PNRReference attaches a booking to an existing Amadeus PNR.
type PNRReference struct {
	// Reference is the record locator, e.g. "JKL789". Required.
	Reference string
	// OriginSystemCode is "GDS" for an Amadeus PNR, and defaults to it.
	OriginSystemCode string
}

// Patterns Amadeus enforces. Checking them here turns a rejected booking into
// an immediate error naming the field.
var (
	namePattern   = regexp.MustCompile(`^[A-Za-z ]+$`)
	emailPattern  = regexp.MustCompile(`^[a-zA-Z0-9_.+-]+@[a-zA-Z0-9-]+\.[a-zA-Z0-9-.]+$`)
	cardPattern   = regexp.MustCompile(`^[0-9]{14,19}$`)
	vendorPattern = regexp.MustCompile(`^[A-Z]{2}$`)
	cvvPattern    = regexp.MustCompile(`^[0-9]{3,4}$`)
	expiryPattern = regexp.MustCompile(`^([0-9]{4}|[0-9]{4}-[0-9]{2})$`)
)

// validate collects every problem with the reservation.
//
// This is the strictest validation in the SDK, deliberately. A malformed search
// costs a round trip; a malformed booking either fails after the guest has been
// told it succeeded, or succeeds on terms nobody intended.
func (r Reservation) validate() error {
	var errs apierr.ValidationErrors

	errs = r.validateGuests(errs)
	errs = r.validateRooms(errs)
	errs = r.Payment.validate(errs)

	if r.Agent.Email == "" {
		errs = errs.Append("Agent.Email", "is required")
	} else if !emailPattern.MatchString(r.Agent.Email) {
		errs = append(errs, apierr.Invalidf("Agent.Email", "%q is not a valid email address", r.Agent.Email))
	}

	if r.Arrival != nil {
		if r.Arrival.CarrierCode == "" {
			errs = errs.Append("Arrival.CarrierCode", "is required when Arrival is set")
		}
		if r.Arrival.FlightNumber == "" {
			errs = errs.Append("Arrival.FlightNumber", "is required when Arrival is set")
		}
		if r.Arrival.DepartureAirport == "" {
			errs = errs.Append("Arrival.DepartureAirport", "is required when Arrival is set")
		}
	}

	if r.AddToPNR != nil && r.AddToPNR.Reference == "" {
		errs = errs.Append("AddToPNR.Reference", "is required when AddToPNR is set")
	}

	return errs.OrNil()
}

func (r Reservation) validateGuests(errs apierr.ValidationErrors) apierr.ValidationErrors {
	if len(r.Guests) == 0 {
		return errs.Append("Guests", "at least one guest is required")
	}

	seen := make(map[int]bool, len(r.Guests))
	for i, guest := range r.Guests {
		field := fmt.Sprintf("Guests[%d]", i)

		// Duplicate IDs would make a room association ambiguous, and Amadeus
		// resolves the ambiguity silently rather than rejecting it.
		if seen[guest.ID] {
			errs = append(errs, apierr.Invalidf(field+".ID",
				"duplicate guest ID %d; each guest needs a unique ID", guest.ID))
		}
		seen[guest.ID] = true

		if guest.FirstName == "" {
			errs = errs.Append(field+".FirstName", "is required")
		} else if !namePattern.MatchString(guest.FirstName) {
			errs = append(errs, apierr.Invalidf(field+".FirstName",
				"%q must be English letters and spaces only", guest.FirstName))
		}

		if guest.LastName == "" {
			errs = errs.Append(field+".LastName", "is required")
		} else if !namePattern.MatchString(guest.LastName) {
			errs = append(errs, apierr.Invalidf(field+".LastName",
				"%q must be English letters and spaces only", guest.LastName))
		}

		if length := len(guest.Title) + len(guest.FirstName) + len(guest.LastName); length > MaxNameLength {
			errs = append(errs, apierr.Invalidf(field,
				"title, first and last name total %d characters; Amadeus allows %d", length, MaxNameLength))
		}

		if guest.Email != "" && !emailPattern.MatchString(guest.Email) {
			errs = append(errs, apierr.Invalidf(field+".Email",
				"%q is not a valid email address", guest.Email))
		}
		if guest.ChildAge != nil && (*guest.ChildAge < 0 || *guest.ChildAge > 17) {
			errs = append(errs, apierr.Invalidf(field+".ChildAge",
				"must be between 0 and 17, got %d", *guest.ChildAge))
		}
		if len(guest.FrequentTraveler) > 1 {
			errs = append(errs, apierr.Invalidf(field+".FrequentTraveler",
				"only the first membership reaches the hotel provider; supply at most one, got %d",
				len(guest.FrequentTraveler)))
		}
	}

	return errs
}

func (r Reservation) validateRooms(errs apierr.ValidationErrors) apierr.ValidationErrors {
	switch {
	case len(r.Rooms) == 0:
		return errs.Append("Rooms", "at least one room is required")
	case len(r.Rooms) > MaxRooms:
		return append(errs, apierr.Invalidf("Rooms",
			"at most %d rooms per order, got %d", MaxRooms, len(r.Rooms)))
	}

	known := make(map[int]bool, len(r.Guests))
	for _, guest := range r.Guests {
		known[guest.ID] = true
	}
	// occupies tracks which guests are already assigned a room, across every
	// room association rather than within one.
	occupies := make(map[int]bool, len(r.Guests))

	for i, room := range r.Rooms {
		field := fmt.Sprintf("Rooms[%d]", i)

		if strings.TrimSpace(room.OfferID) == "" {
			errs = errs.Append(field+".OfferID", "is required")
		}
		if len(room.GuestIDs) == 0 {
			errs = errs.Append(field+".GuestIDs", "at least one guest must occupy the room")
		}

		// A reference to a guest that is not on the reservation is the single
		// easiest mistake to make here, and Amadeus reports it only as a
		// generic invalid-data error.
		for _, id := range room.GuestIDs {
			if !known[id] {
				errs = append(errs, apierr.Invalidf(field+".GuestIDs",
					"guest ID %d is not in Guests", id))
			}
			// A person occupies one room. Amadeus rejects a guest who appears
			// in two room associations as "Ids must be unique", a 400 naming
			// neither the guest nor the room, so it is caught here instead.
			if occupies[id] {
				errs = append(errs, apierr.Invalidf(field+".GuestIDs",
					"guest ID %d already occupies another room; each guest belongs to one room", id))
				continue
			}
			occupies[id] = true
		}
		for id := range room.LoyaltyIDs {
			if !known[id] {
				errs = append(errs, apierr.Invalidf(field+".LoyaltyIDs",
					"guest ID %d is not in Guests", id))
			}
		}

		if len(room.SpecialRequest) > MaxSpecialRequest {
			errs = append(errs, apierr.Invalidf(field+".SpecialRequest",
				"is %d characters; Amadeus allows %d", len(room.SpecialRequest), MaxSpecialRequest))
		}
	}

	return errs
}

func (p Payment) validate(errs apierr.ValidationErrors) apierr.ValidationErrors {
	if p.Method == "" {
		return errs.Append("Payment.Method", "is required")
	}
	if !p.Method.IsValid() {
		return append(errs, apierr.Invalidf("Payment.Method", "%q is not a known payment method", p.Method))
	}

	if p.Method.RequiresCard() {
		if p.Card == nil {
			return errs.Append("Payment.Card", "is required for "+string(p.Method))
		}
		errs = p.Card.validate(errs)
	}

	if p.Method == PaymentVCCBillback {
		if p.BillBack == nil {
			errs = errs.Append("Payment.BillBack", "is required for VCC_BILLBACK")
		} else {
			if p.BillBack.ProviderCode == "" {
				errs = errs.Append("Payment.BillBack.ProviderCode", "is required")
			}
			if p.BillBack.AccountNumber == "" {
				errs = errs.Append("Payment.BillBack.AccountNumber", "is required")
			}
		}
	}

	return errs
}

func (c Card) validate(errs apierr.ValidationErrors) apierr.ValidationErrors {
	if !vendorPattern.MatchString(c.VendorCode) {
		errs = append(errs, apierr.Invalidf("Payment.Card.VendorCode",
			"%q must be two uppercase letters, e.g. VI or CA", c.VendorCode))
	}

	// The card number is never quoted back in an error: it must not reach a
	// log, a terminal or an error-reporting service.
	switch {
	case c.Number == "":
		errs = errs.Append("Payment.Card.Number", "is required")
	case !cardPattern.MatchString(c.Number):
		errs = errs.Append("Payment.Card.Number", "must be 14 to 19 digits")
	case !passesLuhn(c.Number):
		errs = errs.Append("Payment.Card.Number", "failed the Luhn checksum; it was likely mistyped")
	}

	if c.Expiry == "" {
		errs = errs.Append("Payment.Card.Expiry", "is required")
	} else if !expiryPattern.MatchString(c.Expiry) {
		errs = append(errs, apierr.Invalidf("Payment.Card.Expiry",
			"%q must be MMYY or YYYY-MM", c.Expiry))
	}

	if c.SecurityCode != "" && !cvvPattern.MatchString(c.SecurityCode) {
		errs = errs.Append("Payment.Card.SecurityCode", "must be 3 or 4 digits")
	}
	if c.HolderName != "" && !namePattern.MatchString(c.HolderName) {
		errs = append(errs, apierr.Invalidf("Payment.Card.HolderName",
			"%q must be English letters and spaces only", c.HolderName))
	}

	if c.ThreeDS != nil {
		if c.ThreeDS.Version == "" {
			errs = errs.Append("Payment.Card.ThreeDS.Version", "is required")
		}
		if c.ThreeDS.CryptogramValue == "" {
			errs = errs.Append("Payment.Card.ThreeDS.CryptogramValue", "is required")
		}
	}

	return errs
}

// passesLuhn checks a card number against the Luhn checksum.
//
// It catches a transposed or mistyped digit locally rather than after the
// booking round trip, which is worth doing because the failure otherwise
// surfaces as an opaque decline.
func passesLuhn(number string) bool {
	sum := 0
	double := false

	for i := len(number) - 1; i >= 0; i-- {
		digit, err := strconv.Atoi(string(number[i]))
		if err != nil {
			return false
		}
		if double {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
		double = !double
	}

	return sum%10 == 0
}
