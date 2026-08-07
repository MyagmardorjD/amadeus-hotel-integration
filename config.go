package sdk

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/apierr"
)

// Environment selects which Amadeus deployment the SDK talks to.
//
// This was previously a constant compiled into the SDK, with a comment
// instructing you to edit the source before going to production. It is now a
// configuration field, so one binary can be pointed at either.
type Environment string

const (
	// Test is the Amadeus sandbox. Its data is a static subset and its bookings
	// are not real, but it is otherwise the same API.
	Test Environment = "test"
	// Production is the live Amadeus deployment. Bookings made here are real
	// and incur real charges.
	Production Environment = "production"
)

// Host returns the API host root for e, defaulting to the sandbox.
//
// Defaulting to the sandbox is deliberate: a caller who forgets to set the
// environment gets test data, not a real booking. The failure mode of the
// opposite default is charging somebody's card.
func (e Environment) Host() string {
	switch e {
	case Production:
		return "https://travel.api.amadeus.com"
	default:
		return "https://test.travel.api.amadeus.com"
	}
}

// IsValid reports whether e is a recognised environment. The empty string is
// valid and means Test.
func (e Environment) IsValid() bool {
	return e == "" || e == Test || e == Production
}

// String returns the environment name, resolving the empty value to "test".
func (e Environment) String() string {
	if e == "" {
		return string(Test)
	}
	return string(e)
}

// Config holds everything New needs to build a Client.
//
// Only the credentials are required; every other field has a working default.
//
//	client, err := sdk.New(sdk.Config{
//	    ClientID:     os.Getenv("AMADEUS_CLIENT_ID"),
//	    ClientSecret: os.Getenv("AMADEUS_CLIENT_SECRET"),
//	    Environment:  sdk.Production,
//	})
//
// These endpoints require Amadeus Enterprise credentials. Self-service
// credentials from the developer portal are rejected by this host with a 403.
type Config struct {
	// ClientID is the Amadeus API key. Required.
	ClientID string
	// ClientSecret is the Amadeus API secret. Required.
	ClientSecret string
	// Environment selects the sandbox or the live deployment. Defaults to Test.
	Environment Environment

	// HTTPClient, when set, is used for every request. Supply one to control
	// proxying, TLS, connection pooling or instrumentation. Its own timeout
	// applies and Timeout below is ignored.
	HTTPClient *http.Client
	// Timeout bounds each request when HTTPClient is nil. Defaults to 60s,
	// which suits Amadeus's slower wide-radius searches.
	Timeout time.Duration
	// UserAgent identifies your application to Amadeus. Defaults to the SDK's
	// own identifier.
	UserAgent string

	// Logger, when set, receives every request and response at Debug level.
	//
	// It logs the method, URL, status, timing and size of each call. Set your
	// handler to emit Debug to see it; leave the handler at Info or higher and
	// nothing is logged and nothing is spent formatting it.
	//
	// Response bodies are logged only when they earn it: on a failure, and on
	// the booking endpoints. A successful search logs its metadata but not its
	// payload, which keeps a hundred-kilobyte result set and the guest data in
	// a reservation out of the log.
	//
	// Request bodies are redacted before logging: the card number, security
	// code and 3DS cryptogram a booking carries are replaced with a
	// placeholder, and the authentication exchange logs no body at all, so its
	// client secret and access token never reach the log.
	//
	//	sdk.New(sdk.Config{
	//	    ClientID:     id,
	//	    ClientSecret: secret,
	//	    Logger: slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
	//	        Level: slog.LevelDebug,
	//	    })),
	//	})
	Logger *slog.Logger

	// SkipCredentialCheck stops New from verifying the credentials before
	// returning, deferring the first token fetch to the first call. Use it when
	// constructing a client must not perform I/O, such as at package init.
	SkipCredentialCheck bool
}

// defaultUserAgent identifies the SDK when the caller sets none.
const defaultUserAgent = "amadeus-hotel-integration-go"

// validate reports every problem with the config at once, so a caller fixes
// them in one pass rather than one error at a time.
func (c Config) validate() error {
	var errs apierr.ValidationErrors

	if c.ClientID == "" {
		errs = errs.Append("ClientID", "is required")
	}
	if c.ClientSecret == "" {
		errs = errs.Append("ClientSecret", "is required")
	}
	if !c.Environment.IsValid() {
		errs = append(errs, apierr.Invalidf("Environment",
			"%q is not a known environment; use sdk.Test or sdk.Production", c.Environment))
	}
	if c.Timeout < 0 {
		errs = append(errs, apierr.Invalidf("Timeout", "must not be negative, got %s", c.Timeout))
	}

	return errs.OrNil()
}

// userAgent returns the configured agent or the SDK default.
func (c Config) userAgent() string {
	if c.UserAgent == "" {
		return defaultUserAgent
	}
	return c.UserAgent
}
