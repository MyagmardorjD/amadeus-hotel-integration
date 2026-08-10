package amadeus

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
)

// This file is the request/response logging, and its redaction.
//
// Logging raw traffic is a debugging convenience with a sharp edge: the
// create-booking request carries a full card number, its CVV and a 3DS
// cryptogram, and the auth request carries the client secret. Writing those to
// a log is exactly the leak the booking validation is careful never to cause in
// an error message. So request bodies are redacted before they are logged, and
// the auth exchange logs neither its body nor its token.
//
// Response bodies are logged for the two cases that justify the volume and the
// exposure - a failure, and a booking - and omitted otherwise. See
// shouldLogResponseBody. When one is logged it is logged as received, since
// Amadeus returns card numbers already masked and the raw payload is the point.
//
// Everything logs at Debug. Traffic logging is verbose and off unless the
// caller's handler is set to emit Debug, and the redaction cost is skipped
// entirely when it is not.

// sensitiveKeys are the JSON fields redacted out of a logged request body,
// lower-cased for a case-insensitive match. These are the values that must
// never reach a log: the card number, its verification code, the 3DS
// cryptogram, and the OAuth credentials.
var sensitiveKeys = map[string]bool{
	"cardnumber":      true,
	"securitycode":    true,
	"cryptogramvalue": true,
	"client_secret":   true,
	"access_token":    true,
	"authorization":   true,
}

// redactPlaceholder replaces a sensitive value in a logged body.
const redactPlaceholder = "[REDACTED]"

// logRequest records an outgoing request. The body is the domain value about to
// be encoded, redacted before it is rendered.
func (c *Client) logRequest(ctx context.Context, method, url string, body any) {
	if !c.logger.Enabled(ctx, slog.LevelDebug) {
		return
	}

	attrs := []slog.Attr{
		slog.String("method", method),
		slog.String("url", url),
	}
	if body != nil {
		attrs = append(attrs, slog.String("body", redactBody(body)))
	}
	c.logger.LogAttrs(ctx, slog.LevelDebug, "amadeus request", attrs...)
}

// logResponse records a completed response. Every response logs its method,
// URL, status, timing and size; the body is included only when
// shouldLogResponseBody says it earns its place, and is logged as received when
// it is.
func (c *Client) logResponse(ctx context.Context, method, url, path string, status int, started time.Time, body []byte) {
	if !c.logger.Enabled(ctx, slog.LevelDebug) {
		return
	}

	attrs := []slog.Attr{
		slog.String("method", method),
		slog.String("url", url),
		slog.Int("status", status),
		slog.Duration("elapsed", time.Since(started)),
		slog.Int("bytes", len(body)),
	}
	if shouldLogResponseBody(path, status, body) {
		attrs = append(attrs, slog.String("body", string(body)))
	}
	c.logger.LogAttrs(ctx, slog.LevelDebug, "amadeus response", attrs...)
}

// logFailure records a call that failed, with the request that caused it and
// the response that came back, at Error level.
//
// Unlike the Debug traffic log this is deliberately not gated on Debug. Debug
// is off in production, which is exactly where a failed booking has to be
// explicable afterwards, and the response body alone does not say which call
// produced it - the request body is the half that identifies the booking.
//
// The request body is redacted by the same rules as the Debug log. Logging more
// on failure must not become a way for a card number to reach a log.
func (c *Client) logFailure(ctx context.Context, req Request, status int, body []byte) {
	if !c.logger.Enabled(ctx, slog.LevelError) {
		return
	}

	attrs := []slog.Attr{
		slog.String("method", req.method()),
		slog.String("url", c.requestURL(req)),
		slog.Int("status", status),
		slog.String("response", string(body)),
	}
	if req.Body != nil {
		attrs = append(attrs, slog.String("request", redactBody(req.Body)))
	}
	c.logger.LogAttrs(ctx, slog.LevelError, "amadeus call failed", attrs...)
}

// logAuthFailure records a rejected authentication exchange.
//
// It logs the response but never the request: the request carries the client
// secret, and no failure is worth leaking a credential to explain. A failed
// auth response carries no token, so it is safe to log as received.
func (m *tokenManager) logAuthFailure(ctx context.Context, status int, body []byte) {
	if !m.logger.Enabled(ctx, slog.LevelError) {
		return
	}
	m.logger.LogAttrs(ctx, slog.LevelError, "amadeus authentication failed",
		slog.String("url", m.host+tokenPath),
		slog.Int("status", status),
		slog.String("response", string(body)),
	)
}

// bookingPathPrefix identifies the Hotel Booking endpoints.
//
// It is spelled out here rather than imported from the booking package, because
// booking imports this package and the dependency cannot run both ways. The
// transport already knows these endpoints are special: amadeusContentType exists
// for the same reason.
const bookingPathPrefix = "/v2/booking/"

// shouldLogResponseBody reports whether a response body belongs in the log.
//
// Logging every body is more than it is worth. One hotel search runs to
// hundreds of kilobytes, and a booking response carries guest names, email
// addresses and phone numbers, so a log shipped to a third party becomes a
// disclosure. Two cases repay both costs:
//
//   - A failure, whether a non-2xx or one of the 200s Amadeus sends carrying an
//     errors array. The body is the only account of what it objected to, and
//     without it a support ticket has nothing to go on.
//   - A booking call. It took money, so the exact response is the audit trail
//     for the reservation, and is worth keeping even when it succeeded.
//
// Everything else logs method, status, timing and size, which is enough to see
// that a call happened, that it worked, and how long it took.
func shouldLogResponseBody(path string, status int, body []byte) bool {
	if status < 200 || status > 299 {
		return true
	}
	if len(parseDetails(body)) > 0 {
		return true
	}
	return strings.HasPrefix(path, bookingPathPrefix)
}

// redactBody marshals a request body and blanks its sensitive fields.
//
// It fails safe: a body it cannot parse is reported as omitted rather than
// logged raw, because "cannot parse" must never become "logged a card number
// because the shape surprised me".
func redactBody(body any) string {
	raw, err := json.Marshal(body)
	if err != nil {
		return "[body omitted: not encodable]"
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "[body omitted: not JSON]"
	}

	redactValue(decoded)

	out, err := json.Marshal(decoded)
	if err != nil {
		return "[body omitted: not encodable after redaction]"
	}
	return string(out)
}

// redactValue walks a decoded JSON value in place, replacing the value of any
// sensitive key with the placeholder. It recurses through objects and arrays,
// so a card nested under payment.paymentCard.paymentCardInfo is still caught.
func redactValue(v any) {
	switch node := v.(type) {
	case map[string]any:
		for key, child := range node {
			if sensitiveKeys[strings.ToLower(key)] {
				node[key] = redactPlaceholder
				continue
			}
			redactValue(child)
		}
	case []any:
		for _, child := range node {
			redactValue(child)
		}
	}
}
