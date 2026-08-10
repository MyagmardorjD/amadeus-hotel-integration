package amadeus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/techpartners-asia/amadeus-hotel-integration/v2/apierr"
)

// tokenPath is the OAuth2 client-credentials endpoint, relative to the host root.
const tokenPath = "/v1/security/oauth2/token"

// refreshWindow renews the token slightly before it actually expires, so a
// request already in flight never races the expiry.
const refreshWindow = 30 * time.Second

// tokenManager caches an OAuth2 access token and refreshes it when it is missing
// or close to expiring.
//
// It replaces a package-level singleton. As a field of Client it lets two
// clients - test and production, or two tenants - coexist in one process, and
// lets tests supply their own transport.
type tokenManager struct {
	id     string
	secret string
	host   string
	http   *http.Client
	logger *slog.Logger

	mu        sync.Mutex
	cached    string
	expiresAt time.Time
}

// tokenResponse is the OAuth2 grant, in Amadeus's spelling.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	State       string `json:"state"`
}

// token returns a valid access token, fetching or refreshing it if needed.
//
// The lock is held across the HTTP call. That serialises concurrent callers
// during a refresh, which is the intent: a burst of goroutines finding the token
// expired should produce one token request, not one per goroutine.
func (m *tokenManager) token(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cached != "" && time.Now().Before(m.expiresAt.Add(-refreshWindow)) {
		return m.cached, nil
	}

	granted, err := m.authenticate(ctx)
	if err != nil {
		return "", err
	}

	m.cached = granted.AccessToken
	m.expiresAt = time.Now().Add(time.Duration(granted.ExpiresIn) * time.Second)
	return m.cached, nil
}

// invalidate drops the cached token, so the next call fetches a fresh one. It is
// used when Amadeus rejects a token the SDK still believed was valid.
func (m *tokenManager) invalidate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cached = ""
	m.expiresAt = time.Time{}
}

// authenticate performs the client-credentials exchange. The caller holds m.mu.
func (m *tokenManager) authenticate(ctx context.Context) (*tokenResponse, error) {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {m.id},
		"client_secret": {m.secret},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.host+tokenPath, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("amadeus: building token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	// The auth exchange is logged by status alone, never by body. The request
	// form carries client_secret and the response carries the access token;
	// neither belongs in a log, so unlike an API call there is no raw body here.
	m.logger.LogAttrs(ctx, slog.LevelDebug, "amadeus auth request",
		slog.String("url", m.host+tokenPath))

	res, err := m.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("amadeus: authenticating: %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("amadeus: reading token response: %w", err)
	}

	m.logger.LogAttrs(ctx, slog.LevelDebug, "amadeus auth response",
		slog.Int("status", res.StatusCode))

	if res.StatusCode != http.StatusOK {
		// A bad credential arrives here as a 401. Surfacing it as a typed
		// APIError means errors.Is(err, apierr.ErrUnauthorized) works for an
		// authentication failure exactly as it does for a rejected request.
		m.logAuthFailure(ctx, res.StatusCode, body)
		return nil, apierr.New(res.StatusCode, parseDetails(body), string(body))
	}

	var granted tokenResponse
	if err := json.Unmarshal(body, &granted); err != nil {
		return nil, fmt.Errorf("amadeus: decoding token response: %w", err)
	}
	if granted.AccessToken == "" {
		return nil, fmt.Errorf("amadeus: token response contained no access_token")
	}
	return &granted, nil
}
