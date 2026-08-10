// Package apierr holds the error types every SDK call can return.
//
// It is a separate package because errors are part of the public API - callers
// match on them - while the wire DTOs that produce them are not, and live under
// internal/. The root sdk package aliases these names, so callers normally
// write sdk.APIError and never import this package directly.
//
// Two ways to inspect a failure:
//
//	// the common case: which kind of failure was it?
//	if errors.Is(err, apierr.ErrNotFound) { ... }
//
//	// the detailed case: what exactly did Amadeus say?
//	var apiErr *apierr.APIError
//	if errors.As(err, &apiErr) {
//	    for _, d := range apiErr.Details { fmt.Println(d.Code, d.Title, d.Detail) }
//	}
package apierr

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Sentinel errors for the failure kinds callers routinely branch on. Every
// APIError wraps exactly one of these (or none, for statuses outside the set),
// so errors.Is works without inspecting the status code by hand.
var (
	// ErrInvalidRequest is a 400: Amadeus rejected the request as malformed.
	ErrInvalidRequest = errors.New("invalid request")
	// ErrUnauthorized is a 401: the credentials are wrong, or the token expired
	// and could not be refreshed.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrForbidden is a 403: the credentials are valid but not entitled to this
	// resource. Self-service credentials against the Enterprise host land here.
	ErrForbidden = errors.New("forbidden")
	// ErrNotFound is a 404: no such hotel, offer or order.
	ErrNotFound = errors.New("not found")
	// ErrRateLimited is a 429: too many requests. Amadeus enforces both a
	// per-second and a per-month quota.
	ErrRateLimited = errors.New("rate limited")
	// ErrServer is any 5xx: the failure is on Amadeus's side and the request may
	// be worth retrying.
	ErrServer = errors.New("server error")
)

// Detail is a single Amadeus error object. Amadeus returns an array of these,
// because one request can be wrong in several ways at once.
type Detail struct {
	// Status is the HTTP status code Amadeus associated with this error.
	Status int `json:"status"`
	// Code is the machine-readable Amadeus error code, e.g. 38196.
	Code int `json:"code"`
	// Title is a short summary with a 1:1 correspondence to Code.
	Title string `json:"title"`
	// Detail explains this particular occurrence.
	Detail string `json:"detail"`
	// Source identifies the request element that caused the error.
	Source Source `json:"source"`
	// Documentation links to further reading, when Amadeus supplies it.
	Documentation string `json:"documentation,omitempty"`
}

// UnmarshalJSON decodes a Detail, tolerating the two spellings Amadeus uses for
// its numeric fields.
//
// The shopping endpoints send {"code": 477, "status": 400}; the authentication
// and header errors send {"code": "38191", "status": "401"}. A plain int field
// rejects the quoted form, and because the errors array is decoded as a whole,
// one quoted code discards every detail in the response - leaving a bare status
// code and no explanation, in exactly the case where the explanation matters.
//
// A code that is neither form is left as zero rather than failing the decode,
// so the title and detail still reach the caller.
func (d *Detail) UnmarshalJSON(data []byte) error {
	// The alias sheds the method set, so this does not recurse.
	type alias Detail
	var raw struct {
		alias
		Status json.RawMessage `json:"status"`
		Code   json.RawMessage `json:"code"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*d = Detail(raw.alias)
	d.Status = flexibleInt(raw.Status)
	d.Code = flexibleInt(raw.Code)
	return nil
}

// flexibleInt reads a JSON value that may be a number or a quoted number,
// returning zero for anything else.
func flexibleInt(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// Source identifies the request element that triggered an error.
type Source struct {
	// Parameter is the path or query parameter at fault.
	Parameter string `json:"parameter,omitempty"`
	// Pointer is an RFC 6901 JSON Pointer into the request body.
	Pointer string `json:"pointer,omitempty"`
	// Example is a sample valid value, when Amadeus offers one.
	Example string `json:"example,omitempty"`
}

// String renders the source as "parameter=cityCode" or "pointer=/data/guests/0".
func (s Source) String() string {
	switch {
	case s.Parameter != "":
		return "parameter=" + s.Parameter
	case s.Pointer != "":
		return "pointer=" + s.Pointer
	default:
		return ""
	}
}

// APIError is the typed error returned when Amadeus rejects a request.
//
// It wraps a sentinel chosen from the HTTP status, so both of these work:
//
//	errors.Is(err, apierr.ErrNotFound)
//	errors.As(err, &apiErr)
type APIError struct {
	// StatusCode is the HTTP status of the failed response.
	StatusCode int
	// Details holds the structured Amadeus error objects. It is empty when the
	// body was not a standard error envelope - a gateway timeout page, say.
	Details []Detail
	// Body is the raw response body, kept as a fallback for non-standard errors
	// and truncated to a readable length.
	Body string

	// kind is the sentinel this error wraps, derived from StatusCode.
	kind error
}

// New builds an APIError for a status and its parsed details, selecting the
// sentinel it wraps from the status code.
func New(statusCode int, details []Detail, body string) *APIError {
	return &APIError{
		StatusCode: statusCode,
		Details:    details,
		Body:       truncate(body, maxBodyLength),
		kind:       kindFor(statusCode),
	}
}

// maxBodyLength caps the raw body retained on an error. Amadeus error bodies are
// small, but a misrouted request can return a full HTML page, and an error
// message that fills the terminal helps nobody.
const maxBodyLength = 2048

// Error renders the most useful summary available: the first Amadeus detail when
// there is one, the raw body otherwise.
func (e *APIError) Error() string {
	if len(e.Details) == 0 {
		return fmt.Sprintf("amadeus: request failed (status %d): %s", e.StatusCode, e.Body)
	}

	first := e.Details[0]
	var b strings.Builder
	fmt.Fprintf(&b, "amadeus: [%d] %s", first.Code, first.Title)
	if first.Detail != "" {
		b.WriteString(" - ")
		b.WriteString(first.Detail)
	}
	if src := first.Source.String(); src != "" {
		b.WriteString(" (")
		b.WriteString(src)
		b.WriteString(")")
	}

	status := first.Status
	if status == 0 {
		status = e.StatusCode
	}
	fmt.Fprintf(&b, " (status %d)", status)

	if extra := len(e.Details) - 1; extra > 0 {
		fmt.Fprintf(&b, " (+%d more)", extra)
	}
	return b.String()
}

// Unwrap returns the sentinel this error represents, which is what makes
// errors.Is(err, ErrNotFound) work.
func (e *APIError) Unwrap() error { return e.kind }

// Is reports whether target is the sentinel for this error's status. It is
// needed alongside Unwrap only for the nil-kind case, where an unrecognised
// status must not match anything.
func (e *APIError) Is(target error) bool { return e.kind != nil && target == e.kind }

// kindFor maps an HTTP status onto the sentinel it should wrap. Statuses outside
// the set produce a nil kind, so errors.As still recovers the full detail while
// errors.Is matches nothing and cannot mislead.
func kindFor(status int) error {
	switch {
	case status == http.StatusBadRequest:
		return ErrInvalidRequest
	case status == http.StatusUnauthorized:
		return ErrUnauthorized
	case status == http.StatusForbidden:
		return ErrForbidden
	case status == http.StatusNotFound:
		return ErrNotFound
	case status == http.StatusTooManyRequests:
		return ErrRateLimited
	case status >= 500:
		return ErrServer
	default:
		return nil
	}
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "... (truncated)"
}
