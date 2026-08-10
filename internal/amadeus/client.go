// Package amadeus is the SDK's transport: it authenticates, sends requests and
// turns responses into either a decoded envelope or a typed error.
//
// It lives under internal/ together with the wire DTOs, which is what enforces
// the anti-corruption layer. Callers cannot import this package, so they cannot
// come to depend on Amadeus's JSON shapes; only each context's mapper sees both
// sides of the boundary.
package amadeus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/apierr"
)

// maxResponseBytes caps how much of a response the SDK will read. A hotel
// content response for a large property runs to a few hundred kilobytes; this
// leaves generous room while bounding memory if a proxy returns something
// unexpected.
const maxResponseBytes = 32 << 20 // 32 MiB

// defaultTimeout applies when the caller supplies neither a timeout nor their
// own http.Client. Amadeus hotel searches over a wide radius are genuinely slow,
// so this is deliberately generous.
const defaultTimeout = 60 * time.Second

// amadeusContentType is the media type the booking endpoints require on request
// bodies. The search and content endpoints accept plain application/json.
const amadeusContentType = "application/vnd.amadeus+json"

// Options configures a Client.
//
// It duplicates part of the public sdk.Config rather than importing it, because
// the root package imports this one and the dependency cannot run both ways.
// The root package owns the public, documented shape; this is the internal form
// it maps onto.
type Options struct {
	// ClientID and ClientSecret are the Amadeus API credentials.
	ClientID     string
	ClientSecret string
	// Host is the scheme and host root with no version segment or trailing
	// slash, e.g. "https://test.travel.api.amadeus.com". Paths carry their own
	// version, because the four hotel APIs sit on v1, v2 and v3 at once.
	Host string
	// HTTPClient, when set, is used for every request including authentication.
	// This is the seam tests use to serve fixtures without a network.
	HTTPClient *http.Client
	// Timeout applies only when HTTPClient is nil; a supplied client keeps its
	// own timeout.
	Timeout time.Duration
	// UserAgent identifies the caller to Amadeus.
	UserAgent string
	// Logger receives request and response logging at Debug level. Nil means no
	// logging.
	Logger *slog.Logger
}

// Client sends authenticated requests to Amadeus.
//
// It is safe for concurrent use, and holds its own token manager, so several
// clients can coexist in one process.
type Client struct {
	host      string
	http      *http.Client
	tokens    *tokenManager
	userAgent string
	logger    *slog.Logger
}

// NewClient returns a Client for opts. It does not contact Amadeus: the first
// request fetches the token. Callers wanting to verify credentials eagerly
// should call Ping.
func NewClient(opts Options) *Client {
	httpClient := opts.HTTPClient
	if httpClient == nil {
		timeout := opts.Timeout
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		httpClient = &http.Client{Timeout: timeout}
	}

	host := strings.TrimSuffix(opts.Host, "/")

	// A non-nil logger everywhere means the call sites never guard for nil; a
	// discard handler is the cheap default when the caller supplied none.
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	return &Client{
		host:      host,
		http:      httpClient,
		userAgent: opts.UserAgent,
		logger:    logger,
		tokens: &tokenManager{
			id:     opts.ClientID,
			secret: opts.ClientSecret,
			host:   host,
			http:   httpClient,
			logger: logger,
		},
	}
}

// Ping verifies the credentials by obtaining a token, and is what sdk.New uses
// to fail fast on a misconfiguration rather than at the first search.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.tokens.token(ctx)
	return err
}

// Request describes one call to Amadeus.
type Request struct {
	// Method is the HTTP method. Empty means GET.
	Method string
	// Path is the versioned path, e.g. "/v3/shopping/hotel-offers".
	Path string
	// Query holds the query parameters, already rendered as strings.
	Query url.Values
	// Body, when non-nil, is JSON-encoded as the request body.
	Body any
	// AmadeusJSON sends the vnd.amadeus+json content type, which the booking
	// endpoints require and reject requests without.
	AmadeusJSON bool
}

// Envelope is the response shape every Amadeus hotel endpoint shares: the
// payload under "data", with optional errors, warnings and metadata beside it.
type Envelope[T any] struct {
	Data     T               `json:"data"`
	Included json.RawMessage `json:"included,omitempty"`
	Errors   []apierr.Detail `json:"errors,omitempty"`
	Warnings []Warning       `json:"warnings,omitempty"`
	Meta     Meta            `json:"meta"`
	// Dictionaries holds the lookup tables a response refers to rather than
	// inlining. Hotel Search uses it for currency conversion rates, and
	// dropping it makes a requested currency impossible to display: Amadeus
	// returns prices in the hotel's own currency and expects the caller to
	// apply the rate.
	Dictionaries Dictionaries `json:"dictionaries,omitempty"`
}

// Dictionaries is the lookup block that travels beside a response.
type Dictionaries struct {
	// CurrencyConversionLookupRates maps a source currency to the rate for
	// converting it into the currency the caller asked for.
	CurrencyConversionLookupRates map[string]ConversionRate `json:"currencyConversionLookupRates,omitempty"`
}

// ConversionRate is one currency conversion Amadeus supplies.
type ConversionRate struct {
	// Rate is the multiplier, as a decimal string. Amadeus quotes it to
	// sixteen places ("4099.1909999999998035"), most of which is float noise.
	Rate string `json:"rate"`
	// Target is the currency being converted to.
	Target string `json:"target"`
	// TargetDecimalPlaces is the minor-unit precision of the target currency.
	// It is 0 for currencies with no subdivision, such as MNT and JPY, and a
	// converted amount must be rounded to it before being shown or charged.
	TargetDecimalPlaces int `json:"targetDecimalPlaces"`
}

// Warning is a non-blocking problem Amadeus reports alongside a successful
// response, such as a filter it could not apply.
type Warning struct {
	Code   int            `json:"code,omitempty"`
	Title  string         `json:"title,omitempty"`
	Detail string         `json:"detail,omitempty"`
	Source *apierr.Source `json:"source,omitempty"`
}

// Meta is the response metadata, chiefly the result count and pagination links.
type Meta struct {
	Count int    `json:"count,omitempty"`
	Links *Links `json:"links,omitempty"`
}

// Links holds the pagination URLs Amadeus supplies.
type Links struct {
	Self string `json:"self,omitempty"`
	Next string `json:"next,omitempty"`
	Last string `json:"last,omitempty"`
}

// Do sends req and decodes the response envelope into T.
//
// It is a function rather than a method because Go does not allow type
// parameters on methods. This is the single place the result-and-error handling
// lives; before the restructure it was copy-pasted into eleven call sites.
func Do[T any](ctx context.Context, c *Client, req Request) (*Envelope[T], error) {
	body, err := c.send(ctx, req)
	if err != nil {
		return nil, err
	}

	var envelope Envelope[T]
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("amadeus: decoding %s %s: %w", req.method(), req.Path, err)
	}
	return &envelope, nil
}

// DoRaw sends req and returns the undecoded body. Fixture capture uses it, as
// does the one endpoint whose payload does not sit under "data".
func DoRaw(ctx context.Context, c *Client, req Request) ([]byte, error) {
	return c.send(ctx, req)
}

// send performs the request, retrying once if a token the SDK believed valid was
// rejected, and converts a failure response into a typed error.
func (c *Client) send(ctx context.Context, req Request) ([]byte, error) {
	status, body, err := c.attempt(ctx, req)
	if err != nil {
		return nil, err
	}

	// A 401 against a cached token means it was revoked or expired early. One
	// retry with a fresh token turns a spurious failure into a success; a second
	// 401 is a real credential problem and is returned.
	if status == http.StatusUnauthorized {
		c.tokens.invalidate()
		if status, body, err = c.attempt(ctx, req); err != nil {
			return nil, err
		}
	}

	// Amadeus sometimes returns 200 with an errors array and no usable data, so
	// the body is inspected regardless of status.
	details := parseDetails(body)
	if len(details) > 0 {
		c.logFailure(ctx, req, status, body)
		return nil, apierr.New(status, details, string(body))
	}
	if status < 200 || status > 299 {
		c.logFailure(ctx, req, status, body)
		return nil, apierr.New(status, nil, string(body))
	}

	return body, nil
}

// requestURL renders the URL a request will be sent to, for logging a failure
// after the fact. build() constructs the same string for the request itself.
func (c *Client) requestURL(req Request) string {
	target := c.host + req.Path
	if len(req.Query) > 0 {
		target += "?" + req.Query.Encode()
	}
	return target
}

// attempt performs a single authenticated round trip.
func (c *Client) attempt(ctx context.Context, req Request) (int, []byte, error) {
	token, err := c.tokens.token(ctx)
	if err != nil {
		return 0, nil, err
	}

	httpReq, err := c.build(ctx, req, token)
	if err != nil {
		return 0, nil, err
	}

	// httpReq.URL carries the rendered query; req.Body is the pre-encoding
	// value, redacted before it is logged.
	c.logRequest(ctx, httpReq.Method, httpReq.URL.String(), req.Body)

	started := time.Now()
	res, err := c.http.Do(httpReq)
	if err != nil {
		// A cancelled or timed-out context surfaces here; wrapping keeps
		// errors.Is(err, context.DeadlineExceeded) working for the caller.
		return 0, nil, fmt.Errorf("amadeus: %s %s: %w", req.method(), req.Path, err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, fmt.Errorf("amadeus: reading %s %s: %w", req.method(), req.Path, err)
	}

	c.logResponse(ctx, httpReq.Method, httpReq.URL.String(), req.Path, res.StatusCode, started, body)
	return res.StatusCode, body, nil
}

// build assembles the http.Request, including the bearer token and body.
func (c *Client) build(ctx context.Context, req Request, token string) (*http.Request, error) {
	target := c.host + req.Path
	if len(req.Query) > 0 {
		target += "?" + req.Query.Encode()
	}

	var payload io.Reader
	if req.Body != nil {
		encoded, err := json.Marshal(req.Body)
		if err != nil {
			return nil, fmt.Errorf("amadeus: encoding request body for %s: %w", req.Path, err)
		}
		payload = bytes.NewReader(encoded)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.method(), target, payload)
	if err != nil {
		return nil, fmt.Errorf("amadeus: building request for %s: %w", req.Path, err)
	}

	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	if c.userAgent != "" {
		httpReq.Header.Set("User-Agent", c.userAgent)
	}
	if req.Body != nil {
		if req.AmadeusJSON {
			httpReq.Header.Set("Content-Type", amadeusContentType)
		} else {
			httpReq.Header.Set("Content-Type", "application/json")
		}
	}

	return httpReq, nil
}

// method returns the request's HTTP method, defaulting to GET.
func (r Request) method() string {
	if r.Method == "" {
		return http.MethodGet
	}
	return r.Method
}

// parseDetails pulls the Amadeus errors array out of a body, tolerating bodies
// that are not JSON at all - a proxy's HTML error page, say.
func parseDetails(body []byte) []apierr.Detail {
	var envelope struct {
		Errors []apierr.Detail `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil
	}
	return envelope.Errors
}
