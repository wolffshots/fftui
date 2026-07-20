package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strconv"
	"strings"
	"time"
)

// Auth defaults captured from the live web app. Login is a CSRF-protected
// session flow on a *separate* host from the data API:
//
//	GET  {auth}/api/auth/get-csrf-token/   -> {"csrf_token"} + csrftoken cookie
//	POST {auth}/api/auth/login/            -> {"arb_auth_token"} (Max-Age 3600s)
//
// The arb_auth_token is then used as "Token <token>" against the srv data API.
const (
	defaultDataURL  = "https://srv.futureforex.co.za"
	defaultAuthURL  = "https://main.futureforex.co.za"
	defaultAuthOrin = "https://account.futureforex.co.za" // Origin/Referer the API expects
	csrfPath        = "/api/auth/get-csrf-token/"
	loginPath       = "/api/auth/login/"
)

// LiveSource pulls cycles live from the brokerage API. Fetch reads the
// full history from the dashboard's CSV export (cycle_list/<client>/?download=csv);
// the arb.go methods read the live current-cycle status and market spread.
type LiveSource struct {
	Token    string       // raw token or "Token <token>"; if empty, minted via login
	ClientID string       // your client id from the dashboard URL, e.g. "1234"
	BaseURL  string       // data host, e.g. https://srv.futureforex.co.za
	AuthURL  string       // auth host, e.g. https://main.futureforex.co.za
	Client   *http.Client // optional; defaults to a 60s client

	// Credentials for the login flow, used when Token is empty.
	Username string
	Password string
}

// NewLiveSource builds a source from the environment:
//
//	FF_TOKEN      a token ("<t>" or "Token <t>"); skips login if set
//	FF_USERNAME   \ used to mint a token via the CSRF login flow
//	FF_PASSWORD   /
//	FF_CLIENT_ID  client id in /api/cycle_list/<id>/ (optional; auto-detected)
//	FF_BASE_URL   data host   (default https://srv.futureforex.co.za)
//	FF_AUTH_URL   auth host   (default https://main.futureforex.co.za)
func NewLiveSource() *LiveSource {
	clientID := os.Getenv("FF_CLIENT_ID")
	base := os.Getenv("FF_BASE_URL")
	if base == "" {
		base = defaultDataURL
	}
	auth := os.Getenv("FF_AUTH_URL")
	if auth == "" {
		auth = defaultAuthURL
	}
	return &LiveSource{
		Token:    os.Getenv("FF_TOKEN"),
		ClientID: clientID,
		BaseURL:  strings.TrimRight(base, "/"),
		AuthURL:  strings.TrimRight(auth, "/"),
		Client:   &http.Client{Timeout: 60 * time.Second},
		Username: os.Getenv("FF_USERNAME"),
		Password: os.Getenv("FF_PASSWORD"),
	}
}

// ensureToken mints a fresh token from credentials when none is configured. A
// no-op if a token is already present.
func (s *LiveSource) ensureToken(ctx context.Context) error {
	if s.Token != "" {
		return nil
	}
	if s.Username == "" || s.Password == "" {
		return fmt.Errorf("no FF_TOKEN set and no FF_USERNAME/PASSWORD to mint one")
	}

	// A dedicated client with a cookie jar so the csrftoken cookie set by the
	// GET is replayed on the login POST (Django's double-submit CSRF check).
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	timeout := 60 * time.Second
	if s.Client != nil && s.Client.Timeout > 0 {
		timeout = s.Client.Timeout
	}
	client := &http.Client{Timeout: timeout, Jar: jar}

	csrf, err := s.fetchCSRF(ctx, client)
	if err != nil {
		return err
	}
	token, err := s.login(ctx, client, csrf)
	if err != nil {
		return err
	}
	s.Token = token
	return nil
}

// fetchCSRF performs step 1: GET the csrf token (also sets the csrftoken cookie).
func (s *LiveSource) fetchCSRF(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.AuthURL+csrfPath, nil)
	if err != nil {
		return "", err
	}
	s.setBrowserHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("csrf request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("csrf %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var out struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.CSRFToken == "" {
		return "", fmt.Errorf("csrf response missing csrf_token: %s", strings.TrimSpace(string(raw)))
	}
	return out.CSRFToken, nil
}

// login performs step 2: POST credentials with the CSRF header and returns the
// arb_auth_token used against the data API.
func (s *LiveSource) login(ctx context.Context, client *http.Client, csrf string) (string, error) {
	body, _ := json.Marshal(map[string]string{"username": s.Username, "password": s.Password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.AuthURL+loginPath, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	s.setBrowserHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRFToken", csrf)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("login %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	var out struct {
		ArbAuthToken string `json:"arb_auth_token"`
		ValidArbSess bool   `json:"valid_arb_session"`
		OTPStatus    string `json:"otp_status"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode login response: %w", err)
	}
	if out.OTPStatus != "" && out.OTPStatus != "disabled" {
		return "", fmt.Errorf("login requires OTP (otp_status=%q) — not supported", out.OTPStatus)
	}
	if out.ArbAuthToken == "" {
		return "", fmt.Errorf("login succeeded but no arb_auth_token in response: %s", strings.TrimSpace(string(raw)))
	}
	return out.ArbAuthToken, nil
}

// setBrowserHeaders adds the Origin/Referer/Accept the API's CSRF/CORS checks
// expect (the endpoints reject requests without a matching Origin).
func (s *LiveSource) setBrowserHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json, */*")
	req.Header.Set("Origin", defaultAuthOrin)
	req.Header.Set("Referer", defaultAuthOrin+"/login")
}

func (s *LiveSource) httpClient() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// errUnauthorized marks a 401 from the data API so authGet can re-mint the token.
var errUnauthorized = errors.New("unauthorized")

// Fetch pulls the full cycle history via the dashboard's CSV export
// (cycle_list/<id>/?download=csv). Unlike the JSON list endpoint, the export
// carries real End Date, Trade Type and ZAR out columns, so live cycles get the
// correct hold days — and it is the exact format parseCSV already handles for
// the --csv file. authGet re-mints the token once on a 401 so the app's `r`
// refresh still recovers from the ~hourly token expiry.
func (s *LiveSource) Fetch(ctx context.Context) ([]Cycle, error) {
	id, err := s.resolveClientID(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := s.authGet(ctx, fmt.Sprintf("/api/cycle_list/%s/?download=csv", id))
	if err != nil {
		return nil, err
	}
	return parseCSV(bytes.NewReader(raw))
}

// resolveClientID returns the client id for the account-scoped endpoints. When
// FF_CLIENT_ID is set it is used as-is; otherwise it is discovered from the
// account's client_list/ after login and cached on the source. Accounts with no
// arbitrage client — or more than one — return an error asking for FF_CLIENT_ID.
func (s *LiveSource) resolveClientID(ctx context.Context) (string, error) {
	if s.ClientID != "" {
		return s.ClientID, nil
	}
	raw, err := s.authGet(ctx, "/api/client_list/")
	if err != nil {
		return "", fmt.Errorf("discover client id: %w", err)
	}
	ids, err := parseClientIDs(raw)
	if err != nil {
		return "", err
	}
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("no arbitrage client found on this account; set FF_CLIENT_ID")
	case 1:
		s.ClientID = strconv.FormatInt(ids[0], 10)
		return s.ClientID, nil
	default:
		return "", fmt.Errorf("account has %d clients; set FF_CLIENT_ID to choose one", len(ids))
	}
}

// parseClientIDs extracts the client ids from a client_list/ response, accepting
// either the DRF-paginated object ({results:[…]}) or a bare array.
func parseClientIDs(raw []byte) ([]int64, error) {
	type clientRef struct {
		ID int64 `json:"id"`
	}
	var page struct {
		Results []clientRef `json:"results"`
	}
	var items []clientRef
	if json.Unmarshal(raw, &page) == nil && page.Results != nil {
		items = page.Results
	} else if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decode client_list: %s", strings.TrimSpace(string(raw)))
	}
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	return ids, nil
}

// authHeader normalises the token into a full Authorization header value.
func authHeader(token string) string {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(strings.ToLower(token), "token ") {
		return token
	}
	return "Token " + token
}

// flexFloat unmarshals a JSON number that may be encoded as a string.
type flexFloat float64

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = flexFloat(v)
	return nil
}
