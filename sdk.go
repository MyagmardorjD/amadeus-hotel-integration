// Package sdk is a Go client for the Amadeus Hotel APIs: finding hotels,
// pricing stays, describing properties and making reservations.
//
// # Getting started
//
//	client, err := sdk.New(sdk.Config{
//	    ClientID:     os.Getenv("AMADEUS_CLIENT_ID"),
//	    ClientSecret: os.Getenv("AMADEUS_CLIENT_SECRET"),
//	    Environment:  sdk.Test,
//	})
//	if err != nil {
//	    return err
//	}
//
//	hotels, err := client.Inventory.ByCity(ctx, inventory.CityQuery{CityCode: "PAR"})
//
// # Structure
//
// The SDK is organised as four bounded contexts, each a package you import for
// its own types:
//
//   - inventory - which hotels exist, and where.
//   - content   - what a property is like: rooms, facilities, policies, photos.
//   - offers    - what a stay costs, and which rates are bookable.
//   - booking   - turning an offer into a reservation, and managing it after.
//
// They share three value-object packages - money, geo and datetime - plus
// codes for the enumerations Amadeus accepts in search filters, and media for
// images and text.
//
// Amadeus's own JSON shapes are not part of this API. They live under
// internal/, so a caller cannot come to depend on them: prices arrive as
// money.Money rather than decimal strings, dates as datetime.Date rather than
// strings, and enumerations as typed codes rather than free text.
//
// # Errors
//
// Every call can return a typed error. Match on the kind, or inspect the
// detail:
//
//	if errors.Is(err, sdk.ErrNotFound) { ... }
//
//	var apiErr *sdk.APIError
//	if errors.As(err, &apiErr) {
//	    for _, d := range apiErr.Details { log.Println(d.Code, d.Title, d.Detail) }
//	}
//
// Requests the SDK can tell are wrong are rejected before any network call,
// as a ValidationError naming the field. Match those with
// errors.Is(err, sdk.ErrValidation).
//
// # Credentials
//
// These endpoints require Amadeus Enterprise credentials. Self-service
// credentials from the developer portal are rejected with a 403.
package sdk

import (
	"context"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/apierr"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/booking"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/codes"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/content"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/internal/amadeus"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/inventory"
	"github.com/techpartners-asia/amadeus-hotel-integration/v2/offers"
)

// Client is the entry point to the SDK. It holds one authenticated connection
// to Amadeus and exposes a service per bounded context.
//
// A Client is safe for concurrent use. Several may coexist in one process -
// pointed at different environments, or carrying different credentials - since
// each holds its own token.
type Client struct {
	// Inventory finds hotels: by city, by coordinates, by property code, or by
	// name as a user types it.
	Inventory inventory.Service
	// Content describes a property: rooms, facilities, policies, photographs.
	Content content.Service
	// Offers prices a stay and lists its bookable rates.
	Offers offers.Service
	// Booking turns an offer into a reservation, and manages it afterwards.
	// Against Production it spends real money.
	Booking booking.Service

	// Codes lists the values Amadeus accepts in search filters: amenities,
	// star ratings, board types. It is static data compiled into the SDK, so
	// nothing on it calls Amadeus and nothing on it can fail. The equivalent
	// codes.All* functions need no Client at all.
	Codes codes.Catalog

	// transport is retained so Ping can reuse the same authenticated client.
	transport *amadeus.Client
}

// New returns a Client for cfg.
//
// It verifies the credentials before returning, so a misconfiguration fails
// here rather than at the first search. Set Config.SkipCredentialCheck to defer
// that to the first call, for cases where constructing a client must not
// perform I/O.
func New(cfg Config) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	transport := amadeus.NewClient(amadeus.Options{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Host:         cfg.Environment.Host(),
		HTTPClient:   cfg.HTTPClient,
		Timeout:      cfg.Timeout,
		UserAgent:    cfg.userAgent(),
		Logger:       cfg.Logger,
	})

	if !cfg.SkipCredentialCheck {
		if err := transport.Ping(context.Background()); err != nil {
			return nil, err
		}
	}

	return &Client{
		Inventory: inventory.NewService(transport),
		Content:   content.NewService(transport),
		Offers:    offers.NewService(transport),
		Booking:   booking.NewService(transport),
		Codes:     codes.NewCatalog(),
		transport: transport,
	}, nil
}

// Ping verifies that the credentials still authenticate, for a health check.
func (c *Client) Ping(ctx context.Context) error { return c.transport.Ping(ctx) }

// Error types and sentinels, re-exported so callers need only import this
// package to handle a failure.
type (
	// APIError is returned when Amadeus rejects a request. See apierr.APIError.
	APIError = apierr.APIError
	// ErrorDetail is one structured Amadeus error object.
	ErrorDetail = apierr.Detail
	// ValidationError is returned when the SDK rejects a request before
	// sending it, naming the offending field.
	ValidationError = apierr.ValidationError
	// ValidationErrors is a set of validation failures reported together.
	ValidationErrors = apierr.ValidationErrors
)

// Sentinels for the failure kinds callers branch on, matched with errors.Is.
var (
	// ErrValidation marks a request the SDK rejected before sending.
	ErrValidation = apierr.ErrValidation
	// ErrInvalidRequest is an Amadeus 400.
	ErrInvalidRequest = apierr.ErrInvalidRequest
	// ErrUnauthorized is a 401: wrong credentials, or a token that could not
	// be refreshed.
	ErrUnauthorized = apierr.ErrUnauthorized
	// ErrForbidden is a 403. Self-service credentials against the Enterprise
	// host land here.
	ErrForbidden = apierr.ErrForbidden
	// ErrNotFound is a 404: no such hotel, offer or order. An expired offer ID
	// also produces this.
	ErrNotFound = apierr.ErrNotFound
	// ErrRateLimited is a 429.
	ErrRateLimited = apierr.ErrRateLimited
	// ErrServer is any 5xx, and is the class of failure worth retrying.
	ErrServer = apierr.ErrServer
)
