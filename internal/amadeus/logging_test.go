package amadeus

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// debugLogger returns a logger writing JSON to buf at Debug level, which is
// what a caller wanting request/response logging would configure.
func debugLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestRequestAndResponseAreLogged(t *testing.T) {
	var buf bytes.Buffer
	stub := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, `{"data":{"id":"MC","name":"Marriott"}}`)
	})

	client := NewClient(Options{
		ClientID: "id", ClientSecret: "secret",
		Host: stub.URL, HTTPClient: stub.Server.Client(),
		Logger: debugLogger(&buf),
	})

	if _, err := Do[payload](context.Background(), client, Request{Path: "/v3/hotels"}); err != nil {
		t.Fatalf("Do: %v", err)
	}

	logged := buf.String()
	if !strings.Contains(logged, "amadeus request") {
		t.Error("the request was not logged")
	}
	if !strings.Contains(logged, "amadeus response") {
		t.Error("the response was not logged")
	}
	if !strings.Contains(logged, "/v3/hotels") {
		t.Error("the request URL was not logged")
	}
	// A successful search logs that the call happened and how big it was, but
	// not the payload: those run to hundreds of kilobytes and carry guest data.
	if strings.Contains(logged, "Marriott") {
		t.Error("a successful non-booking response body was logged")
	}
	if !strings.Contains(logged, `"status":200`) || !strings.Contains(logged, `"bytes":`) {
		t.Errorf("the response metadata was not logged: %s", logged)
	}
}

func TestFailedResponseBodyIsLogged(t *testing.T) {
	// A failure is the case the log exists for: the body is the only account of
	// what Amadeus objected to.
	var buf bytes.Buffer
	stub := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 400, `{"errors":[{"code":477,"title":"INVALID FORMAT"}]}`)
	})

	client := NewClient(Options{
		ClientID: "id", ClientSecret: "secret",
		Host: stub.URL, HTTPClient: stub.Server.Client(),
		Logger: debugLogger(&buf),
	})

	if _, err := Do[payload](context.Background(), client, Request{Path: "/v3/hotels"}); err == nil {
		t.Fatal("Do: expected an error for a 400")
	}

	if !strings.Contains(buf.String(), "INVALID FORMAT") {
		t.Errorf("the failure body was not logged: %s", buf.String())
	}
}

func TestTwoHundredCarryingErrorsIsLoggedInFull(t *testing.T) {
	// Amadeus reports some failures as 200 with an errors array, which send()
	// turns into an error. Those bodies are failures and must be logged too.
	var buf bytes.Buffer
	stub := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, `{"errors":[{"code":1257,"title":"NO ROOMS AVAILABLE"}]}`)
	})

	client := NewClient(Options{
		ClientID: "id", ClientSecret: "secret",
		Host: stub.URL, HTTPClient: stub.Server.Client(),
		Logger: debugLogger(&buf),
	})

	if _, err := Do[payload](context.Background(), client, Request{Path: "/v3/hotels"}); err == nil {
		t.Fatal("Do: expected an error for a 200 carrying errors")
	}

	if !strings.Contains(buf.String(), "NO ROOMS AVAILABLE") {
		t.Errorf("a 200 carrying errors was not logged in full: %s", buf.String())
	}
}

func TestBookingResponseBodyIsLogged(t *testing.T) {
	// A booking took money. Its exact response is the audit trail, so it is
	// logged in full even on success.
	var buf bytes.Buffer
	stub := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 201, `{"data":{"id":"ORDER1","name":"CONFIRMED"}}`)
	})

	client := NewClient(Options{
		ClientID: "id", ClientSecret: "secret",
		Host: stub.URL, HTTPClient: stub.Server.Client(),
		Logger: debugLogger(&buf),
	})

	_, err := Do[payload](context.Background(), client, Request{
		Method: http.MethodPost, Path: "/v2/booking/hotel-orders", AmadeusJSON: true,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if !strings.Contains(buf.String(), "ORDER1") {
		t.Errorf("the booking response body was not logged: %s", buf.String())
	}
}

func TestRetrievingAnOrderLogsItsBody(t *testing.T) {
	// Every /v2/booking/ path counts, not just the POST that creates an order.
	var buf bytes.Buffer
	stub := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, `{"data":{"id":"ORDER1","name":"CONFIRMED"}}`)
	})

	client := NewClient(Options{
		ClientID: "id", ClientSecret: "secret",
		Host: stub.URL, HTTPClient: stub.Server.Client(),
		Logger: debugLogger(&buf),
	})

	if _, err := Do[payload](context.Background(), client, Request{
		Path: "/v2/booking/hotel-orders/ORDER1",
	}); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if !strings.Contains(buf.String(), "ORDER1") {
		t.Errorf("an order retrieval was not logged in full: %s", buf.String())
	}
}

func TestCardNumberIsRedactedFromARequestBody(t *testing.T) {
	// The security-critical case. A booking request carries a full card number,
	// its CVV and a 3DS cryptogram. None may reach the log. This is the same
	// guarantee the booking validation makes about error messages, extended to
	// the request log.
	var buf bytes.Buffer
	stub := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 201, `{"data":{"id":"ORDER1"}}`)
	})

	client := NewClient(Options{
		ClientID: "id", ClientSecret: "secret",
		Host: stub.URL, HTTPClient: stub.Server.Client(),
		Logger: debugLogger(&buf),
	})

	const pan = "4111111111111111"
	const cvv = "123"
	const cryptogram = "AAABBWcSNIAAAAABJ1I0gAAAAAA="

	body := map[string]any{
		"data": map[string]any{
			"payment": map[string]any{
				"paymentCard": map[string]any{
					"paymentCardInfo": map[string]any{
						"vendorCode":   "VI",
						"cardNumber":   pan,
						"securityCode": cvv,
						"holderName":   "ADA LOVELACE",
					},
					"threeDomainSecure": map[string]any{
						"cryptogramValue": cryptogram,
					},
				},
			},
		},
	}

	_, err := Do[payload](context.Background(), client, Request{
		Method: http.MethodPost, Path: "/v2/booking/hotel-orders",
		Body: body, AmadeusJSON: true,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	logged := buf.String()
	for _, secret := range []string{pan, cvv, cryptogram} {
		if strings.Contains(logged, secret) {
			t.Errorf("a sensitive value leaked into the log: %q", secret)
		}
	}
	if !strings.Contains(logged, redactPlaceholder) {
		t.Error("nothing was redacted; the card fields should have been")
	}
	// Non-sensitive fields must survive, or the log is useless for debugging.
	if !strings.Contains(logged, "ADA LOVELACE") || !strings.Contains(logged, `\"vendorCode\":\"VI\"`) {
		t.Error("redaction removed non-sensitive fields too")
	}
}

func TestAuthExchangeLogsNoSecretOrToken(t *testing.T) {
	// The auth request carries the client secret; the response carries the
	// access token. Neither may be logged, so the auth flow logs status alone.
	var buf bytes.Buffer
	stub := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, `{"data":{}}`)
	})

	client := NewClient(Options{
		ClientID: "the-client-id", ClientSecret: "the-client-secret",
		Host: stub.URL, HTTPClient: stub.Server.Client(),
		Logger: debugLogger(&buf),
	})

	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	logged := buf.String()
	if !strings.Contains(logged, "amadeus auth request") {
		t.Error("the auth request was not logged at all")
	}
	if strings.Contains(logged, "the-client-secret") {
		t.Error("the client secret leaked into the log")
	}
	// The token the stub issues is "token-1"; it must not appear.
	if strings.Contains(logged, "token-1") {
		t.Error("the access token leaked into the log")
	}
}

func TestNilLoggerLogsNothingAndDoesNotPanic(t *testing.T) {
	// The default: no logger configured. Calls must work and produce no output.
	stub := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, `{"data":{}}`)
	})
	client := NewClient(Options{
		ClientID: "id", ClientSecret: "secret",
		Host: stub.URL, HTTPClient: stub.Server.Client(),
		// Logger deliberately nil.
	})

	if _, err := Do[payload](context.Background(), client, Request{Path: "/v3/x"}); err != nil {
		t.Fatalf("Do with no logger: %v", err)
	}
}

func TestNothingIsLoggedBelowDebugLevel(t *testing.T) {
	// A handler at Info discards Debug records, and the redaction cost is
	// skipped entirely. Verify the guard actually suppresses output.
	var buf bytes.Buffer
	infoLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	stub := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, `{"data":{}}`)
	})
	client := NewClient(Options{
		ClientID: "id", ClientSecret: "secret",
		Host: stub.URL, HTTPClient: stub.Server.Client(),
		Logger: infoLogger,
	})

	if _, err := Do[payload](context.Background(), client, Request{Path: "/v3/x"}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("logged at Info level: %s", buf.String())
	}
}

// Unit tests of the redaction itself, independent of the transport.

func TestRedactValueWalksNestedStructures(t *testing.T) {
	body := map[string]any{
		"cardNumber": "4111111111111111", // top level
		"nested": map[string]any{
			"securityCode": "123",
			"deep": []any{
				map[string]any{"cryptogramValue": "secret", "keep": "visible"},
			},
		},
		"list": []any{
			map[string]any{"client_secret": "hunter2"},
		},
	}

	got := redactBody(body)

	for _, secret := range []string{"4111111111111111", "123", "hunter2"} {
		if strings.Contains(got, secret) {
			t.Errorf("nested secret %q survived redaction: %s", secret, got)
		}
	}
	if strings.Contains(got, `"cryptogramValue":"secret"`) {
		t.Errorf("deep cryptogram survived: %s", got)
	}
	if !strings.Contains(got, `"keep":"visible"`) {
		t.Errorf("a non-sensitive sibling was removed: %s", got)
	}
}

func TestRedactionIsCaseInsensitive(t *testing.T) {
	// Amadeus uses camelCase, but a defensive redactor should not be fooled by
	// a differently-cased key.
	for _, key := range []string{"cardNumber", "CARDNUMBER", "CardNumber", "cardnumber"} {
		got := redactBody(map[string]any{key: "4111111111111111"})
		if strings.Contains(got, "4111111111111111") {
			t.Errorf("key %q was not redacted: %s", key, got)
		}
	}
}

func TestUnparseableBodyIsOmittedNotLoggedRaw(t *testing.T) {
	// Fail safe: a body the redactor cannot parse must be dropped, never
	// emitted raw, or a surprising shape becomes a leak.
	unencodable := map[string]any{"fn": func() {}} // functions do not marshal
	got := redactBody(unencodable)

	if !strings.HasPrefix(got, "[body omitted") {
		t.Errorf("an unencodable body should be omitted, got %q", got)
	}
}

func TestFailedCallIsLoggedAtErrorLevelWithBothBodies(t *testing.T) {
	// The Debug traffic log is off in production. A failure still has to be
	// diagnosable, and the request is the half that says which call failed, so
	// a failed call logs request and response at Error regardless of Debug.
	var buf bytes.Buffer
	infoLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	stub := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 400, `{"errors":[{"code":477,"title":"INVALID FORMAT"}]}`)
	})
	client := NewClient(Options{
		ClientID: "id", ClientSecret: "secret",
		Host: stub.URL, HTTPClient: stub.Server.Client(),
		Logger: infoLogger,
	})

	_, err := Do[payload](context.Background(), client, Request{
		Method: http.MethodPost, Path: "/v2/booking/hotel-orders",
		Body: map[string]any{"data": map[string]any{"offerId": "OFFER1"}}, AmadeusJSON: true,
	})
	if err == nil {
		t.Fatal("expected an error")
	}

	logged := buf.String()
	if !strings.Contains(logged, "amadeus call failed") {
		t.Fatalf("no failure was logged at Error level: %s", logged)
	}
	if !strings.Contains(logged, "INVALID FORMAT") {
		t.Error("the response body was not logged with the failure")
	}
	if !strings.Contains(logged, "OFFER1") {
		t.Error("the request body was not logged with the failure")
	}
	if !strings.Contains(logged, "/v2/booking/hotel-orders") {
		t.Error("the URL was not logged with the failure")
	}
}

func TestFailureLogStillRedactsTheCard(t *testing.T) {
	// Logging the request on failure must not become a way to leak a card
	// number, so the failure log redacts exactly as the Debug log does.
	var buf bytes.Buffer
	infoLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	stub := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 400, `{"errors":[{"code":477,"title":"INVALID FORMAT"}]}`)
	})
	client := NewClient(Options{
		ClientID: "id", ClientSecret: "secret",
		Host: stub.URL, HTTPClient: stub.Server.Client(),
		Logger: infoLogger,
	})

	const pan = "4111111111111111"
	_, _ = Do[payload](context.Background(), client, Request{
		Method: http.MethodPost, Path: "/v2/booking/hotel-orders",
		Body: map[string]any{"payment": map[string]any{"cardNumber": pan, "holderName": "ADA"}},
	})

	logged := buf.String()
	if strings.Contains(logged, pan) {
		t.Error("the card number leaked into the failure log")
	}
	if !strings.Contains(logged, redactPlaceholder) {
		t.Error("the failure log did not redact the card")
	}
	if !strings.Contains(logged, "ADA") {
		t.Error("redaction removed the non-sensitive fields from the failure log")
	}
}

func TestSuccessLogsNothingAtErrorLevel(t *testing.T) {
	var buf bytes.Buffer
	infoLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	stub := newStubServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, `{"data":{"id":"MC"}}`)
	})
	client := NewClient(Options{
		ClientID: "id", ClientSecret: "secret",
		Host: stub.URL, HTTPClient: stub.Server.Client(),
		Logger: infoLogger,
	})

	if _, err := Do[payload](context.Background(), client, Request{Path: "/v3/x"}); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("a successful call logged at Error: %s", buf.String())
	}
}
