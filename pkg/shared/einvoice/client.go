package einvoice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client represents the e-invoice API client
type Client struct {
	config     Config
	session    *Session
	httpClient *http.Client
}

// NewClient creates a new e-invoice API client
func NewClient(config Config) *Client {
	return &Client{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Authenticate authenticates with the e-invoice API and obtains a token
func (c *Client) Authenticate() error {
	endpoint := fmt.Sprintf("%s?email=%s",
		c.config.AuthEndpoint,
		url.QueryEscape(c.config.Email))

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("username", c.config.Username)
	req.Header.Set("password", c.config.Password)
	req.Header.Set("ip_address", c.config.IPAddress)
	req.Header.Set("client_id", c.config.ClientID)
	req.Header.Set("client_secret", c.config.ClientSecret)
	req.Header.Set("gstin", c.config.GSTIN)

	// Log the request as plain JSON
	authReq := map[string]string{
		"endpoint":      endpoint,
		"username":      c.config.Username,
		"password":      c.config.Password, // Added for debugging
		"ip_address":    c.config.IPAddress,
		"client_id":     c.config.ClientID,
		"client_secret": c.config.ClientSecret,
		"gstin":         c.config.GSTIN,
	}
	if rb, jerr := json.Marshal(authReq); jerr == nil {
		fmt.Println("[Authenticate] Request JSON:")
		fmt.Println(string(rb))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Log the response as plain JSON
	fmt.Println("[Authenticate] Response JSON:")
	fmt.Println(string(body))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("authentication failed with status %d: %s", resp.StatusCode, string(body))
	}

	var authResp AuthResponse
	if err := json.Unmarshal(body, &authResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !isSuccessStatus(authResp.StatusCode) {
		return fmt.Errorf("authentication failed: %s", authResp.StatusDesc)
	}

	// Parse token expiry
	tokenExpiry, err := time.Parse("2006-01-02 15:04:05", authResp.Data.TokenExpiry)
	if err != nil {
		// Default to 6 hours if parsing fails
		tokenExpiry = time.Now().Add(6 * time.Hour)
	}

	c.session = &Session{
		AuthToken:   authResp.Data.AuthToken,
		Sek:         authResp.Data.Sek,
		ClientID:    authResp.Data.ClientID,
		TokenExpiry: tokenExpiry,
	}

	return nil
}

// IsAuthenticated checks if the client has a valid session
func (c *Client) IsAuthenticated() bool {
	if c.session == nil {
		return false
	}
	return time.Now().Before(c.session.TokenExpiry)
}

// EnsureAuthenticated ensures the client is authenticated, re-authenticating if necessary
func (c *Client) EnsureAuthenticated() error {
	if !c.IsAuthenticated() {
		return c.Authenticate()
	}
	return nil
}

// GenerateIRN calls the GENERATE IRN API and returns the response
func (c *Client) GenerateIRN(request GenerateIRNRequest) (*GenerateIRNResponse, error) {
	if err := c.EnsureAuthenticated(); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	endpoint := fmt.Sprintf("%s?email=%s",
		c.config.GenerateIRNEndpoint,
		url.QueryEscape(c.config.Email))

	// Marshal request body to JSON
	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Log the request as plain JSON
	fmt.Println("[GenerateIRN] Request JSON:")
	fmt.Println(string(requestBody))

	// Create POST request with body
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ip_address", c.config.IPAddress)
	req.Header.Set("client_id", c.config.ClientID)
	req.Header.Set("client_secret", c.config.ClientSecret)
	req.Header.Set("username", c.config.Username)
	req.Header.Set("auth-token", c.session.AuthToken)
	req.Header.Set("gstin", c.config.GSTIN)

	// Log endpoint and request headers for debugging
	fmt.Println("[GenerateIRN] Endpoint:", endpoint)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Log the response status and headers
	fmt.Println("[GenerateIRN] Response Status:", resp.StatusCode)
	if rh, jerr := json.Marshal(resp.Header); jerr == nil {
		fmt.Println("[GenerateIRN] Response Headers JSON:")
		fmt.Println(string(rh))
	}

	// Log raw response for debugging (no file writes)
	fmt.Println("[GenerateIRN] Raw response length:", len(body))

	// Log the response as plain JSON
	fmt.Println("[GenerateIRN] Response JSON:")
	fmt.Println(string(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	if len(body) == 0 {
		return nil, fmt.Errorf("empty response body from GenerateIRN (status %d)", resp.StatusCode)
	}

	var irnResp GenerateIRNResponse
	if err := json.Unmarshal(body, &irnResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w; body: %s", err, string(body))
	}

	return &irnResp, nil
}

// GetSession returns the current session (for debugging/logging)
func (c *Client) GetSession() *Session {
	return c.session
}

// CancelIRN cancels an existing IRN
func (c *Client) CancelIRN(request CancelIRNRequest) (*CancelIRNResponse, error) {
	if err := c.EnsureAuthenticated(); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	endpoint := fmt.Sprintf("%s?email=%s",
		c.config.CancelEndpoint,
		url.QueryEscape(c.config.Email))

	bodyBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Log the request as plain JSON
	fmt.Println("[CancelIRN] Request JSON:")
	fmt.Println(string(bodyBytes))

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ip_address", c.config.IPAddress)
	req.Header.Set("client_id", c.config.ClientID)
	req.Header.Set("client_secret", c.config.ClientSecret)
	req.Header.Set("username", c.config.Username)
	req.Header.Set("auth-token", c.session.AuthToken)
	req.Header.Set("gstin", c.config.GSTIN)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Log the response as plain JSON
	fmt.Println("[CancelIRN] Response JSON:")
	fmt.Println(string(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var cancelResp CancelIRNResponse
	if err := json.Unmarshal(body, &cancelResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &cancelResp, nil
}

// GenerateEwayBill generates an e-way bill for an existing IRN
func (c *Client) GenerateEwayBill(request GenerateEwayBillRequest) (*GenerateEwayBillResponse, error) {
	if err := c.EnsureAuthenticated(); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	endpoint := fmt.Sprintf("%s?email=%s",
		c.config.EwayBillEndpoint,
		url.QueryEscape(c.config.Email))

	bodyBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Log the request as plain JSON
	fmt.Println("[GenerateEwayBill] Request JSON:")
	fmt.Println(string(bodyBytes))

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ip_address", c.config.IPAddress)
	req.Header.Set("client_id", c.config.ClientID)
	req.Header.Set("client_secret", c.config.ClientSecret)
	req.Header.Set("username", c.config.Username)
	req.Header.Set("auth-token", c.session.AuthToken)
	req.Header.Set("gstin", c.config.GSTIN)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Log the response as plain JSON
	fmt.Println("[GenerateEwayBill] Response JSON:")
	fmt.Println(string(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var ewayResp GenerateEwayBillResponse
	if err := json.Unmarshal(body, &ewayResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &ewayResp, nil
}

// CancelEwayBill cancels an existing e-way bill using standalone API
func (c *Client) CancelEwayBill(request CancelEwayBillRequest) (*CancelEwayBillResponse, error) {
	if err := c.EnsureAuthenticated(); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	endpoint := fmt.Sprintf("%s?email=%s",
		c.config.EwayBillCancelEndpoint,
		url.QueryEscape(c.config.Email))

	bodyBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Log the request as plain JSON
	fmt.Println("[CancelEwayBill] Request JSON:")
	fmt.Println(string(bodyBytes))

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ip_address", c.config.IPAddress)
	req.Header.Set("client_id", c.config.ClientID)
	req.Header.Set("client_secret", c.config.ClientSecret)
	req.Header.Set("username", c.config.Username)
	req.Header.Set("auth-token", c.session.AuthToken)
	req.Header.Set("gstin", c.config.GSTIN)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Log the response as plain JSON
	fmt.Println("[CancelEwayBill] Response JSON:")
	fmt.Println(string(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var cancelResp CancelEwayBillResponse
	if err := json.Unmarshal(body, &cancelResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &cancelResp, nil
}

// AuthenticateEwayBill authenticates with the standalone e-way bill API
func (c *Client) AuthenticateEwayBill() error {
	endpoint := fmt.Sprintf("%s?email=%s&username=%s&password=%s",
		c.config.EwayBillAuthEndpoint,
		url.QueryEscape(c.config.Email),
		url.QueryEscape(c.config.Username),
		url.QueryEscape(c.config.Password))

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers (not username/password - those are in query params)
	req.Header.Set("ip_address", c.config.IPAddress)
	req.Header.Set("client_id", c.config.ClientID)
	req.Header.Set("client_secret", c.config.ClientSecret)
	req.Header.Set("gstin", c.config.GSTIN)

	// Log the request
	authReq := map[string]string{
		"endpoint":      endpoint,
		"username":      c.config.Username,
		"password":      c.config.Password,
		"ip_address":    c.config.IPAddress,
		"client_id":     c.config.ClientID,
		"client_secret": c.config.ClientSecret,
		"gstin":         c.config.GSTIN,
	}
	if rb, jerr := json.Marshal(authReq); jerr == nil {
		fmt.Println("[AuthenticateEwayBill] Request JSON:")
		fmt.Println(string(rb))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Log the response
	fmt.Println("[AuthenticateEwayBill] Response JSON:")
	fmt.Println(string(body))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("authentication failed with status %d: %s", resp.StatusCode, string(body))
	}

	var authResp AuthResponse
	if err := json.Unmarshal(body, &authResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !isSuccessStatus(authResp.StatusCode) {
		return fmt.Errorf("authentication failed: %s", authResp.StatusDesc)
	}

	// Parse token expiry
	tokenExpiry, err := time.Parse("2006-01-02 15:04:05", authResp.Data.TokenExpiry)
	if err != nil {
		tokenExpiry = time.Now().Add(6 * time.Hour)
	}

	c.session = &Session{
		AuthToken:   authResp.Data.AuthToken,
		Sek:         authResp.Data.Sek,
		ClientID:    authResp.Data.ClientID,
		TokenExpiry: tokenExpiry,
	}

	return nil
}

// GenerateStandaloneEwayBill generates a standalone e-way bill without IRN
func (c *Client) GenerateStandaloneEwayBill(request StandaloneEwayBillRequest) (*StandaloneEwayBillResponse, error) {
	if err := c.EnsureAuthenticated(); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	endpoint := fmt.Sprintf("%s?email=%s",
		c.config.EwayBillGenerateEndpoint,
		url.QueryEscape(c.config.Email))

	bodyBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Log the request
	fmt.Println("[GenerateStandaloneEwayBill] Request JSON:")
	fmt.Println(string(bodyBytes))

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ip_address", c.config.IPAddress)
	req.Header.Set("client_id", c.config.ClientID)
	req.Header.Set("client_secret", c.config.ClientSecret)
	req.Header.Set("username", c.config.Username)
	req.Header.Set("auth-token", c.session.AuthToken)
	req.Header.Set("gstin", c.config.GSTIN)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Log the response
	fmt.Println("[GenerateStandaloneEwayBill] Response JSON:")
	fmt.Println(string(body))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var ewayResp StandaloneEwayBillResponse
	if err := json.Unmarshal(body, &ewayResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &ewayResp, nil
}

// GetGSTNDetails fetches GSTN business details for validation
func (c *Client) GetGSTNDetails(gstin string) (*GSTNDetailsResponse, error) {
	if err := c.EnsureAuthenticated(); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Build endpoint with param1 for GSTIN and email
	endpoint := fmt.Sprintf("%s?param1=%s&email=%s",
		c.config.GSTNDetailsEndpoint,
		url.QueryEscape(gstin),
		url.QueryEscape(c.config.Email))

	// Log the request
	fmt.Println("[GetGSTNDetails] Request:")
	fmt.Printf("Endpoint: %s\n", endpoint)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("ip_address", c.config.IPAddress)
	req.Header.Set("client_id", c.config.ClientID)
	req.Header.Set("client_secret", c.config.ClientSecret)
	req.Header.Set("username", c.config.Username)
	req.Header.Set("auth-token", c.session.AuthToken)
	req.Header.Set("gstin", c.config.GSTIN)

	fmt.Printf("Headers: ip_address=%s, client_id=%s, username=%s, gstin=%s, auth-token=%s\n",
		c.config.IPAddress, c.config.ClientID, c.config.Username, c.config.GSTIN, c.session.AuthToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Log the response
	fmt.Println("[GetGSTNDetails] Response JSON:")
	fmt.Println(string(body))
	fmt.Printf("[GetGSTNDetails] Response Status: %d\n", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var gstnResp GSTNDetailsResponse
	if err := json.Unmarshal(body, &gstnResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &gstnResp, nil
}
