package colab

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// notebookHash derives a valid Colab assignment notebook hash (nbh) from an
// identifier. Colab expects a web-safe base64-encoded SHA256 digest (44 chars),
// matching colab-vscode's uuidToWebSafeBase64.
func notebookHash(identifier string) string {
	sum := sha256.Sum256([]byte(identifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Const default Colab backend domains used by the reference implementation.
const (
	DefaultColabDomain     = "https://colab.research.google.com"
	DefaultColabGapiDomain = "https://colab.pa.googleapis.com"
	TunEndpoint            = "/tun/m"
)

// Default HTTP headers used by the Colab backend.
const (
	HeaderAuthorization = "Authorization"
	HeaderClientAgent   = "X-Colab-Client-Agent"
	HeaderTunnel        = "X-Colab-Tunnel"
	HeaderXSRF          = "X-Goog-Colab-Token"
	HeaderProxyToken    = "X-Colab-Runtime-Proxy-Token"
)

type RuntimeSpec struct {
	Variant      string
	Accelerator  string
	Shape        int
	Version      string
	NotebookHash string
}

type RuntimeProxyInfo struct {
	Token                 string `json:"token"`
	TokenExpiresInSeconds int    `json:"tokenExpiresInSeconds"`
	URL                   string `json:"url"`
}

type Assignment struct {
	Endpoint         string           `json:"endpoint"`
	Accelerator      string           `json:"accelerator"`
	Variant          string           `json:"variant"`
	MachineShape     int              `json:"machineShape"`
	RuntimeProxyInfo RuntimeProxyInfo `json:"runtimeProxyInfo"`
	RuntimeVersion   string           `json:"runtimeVersion"`
}

type UserInfo struct {
	SubscriptionTier       string            `json:"subscriptionTier"`
	EligibleAccelerators   []AcceleratorInfo `json:"eligibleAccelerators"`
	IneligibleAccelerators []AcceleratorInfo `json:"ineligibleAccelerators"`
}

type AcceleratorInfo struct {
	Variant string   `json:"variant"`
	Models  []string `json:"models"`
}

// ColabServerClient talks to the Colab v1 backend, mirroring the reference
// implementation in googlecolab/colab-vscode. It keeps the endpoint paths,
// header names and the GET-then-POST XSRF choreography behind one seam.
type ColabServerClient struct {
	ColabDomain     string
	ColabGapiDomain string
	Client          HTTPDoer
	ClientAgent     string
	GetAccessToken  func() (string, error)
	OnAuthError     func() error
}

func NewColabServerClient(colabDomain, colabGapiDomain string, client HTTPDoer) *ColabServerClient {
	if strings.TrimSpace(colabDomain) == "" {
		colabDomain = DefaultColabDomain
	}
	if strings.TrimSpace(colabGapiDomain) == "" {
		colabGapiDomain = DefaultColabGapiDomain
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &ColabServerClient{ColabDomain: strings.TrimRight(colabDomain, "/"), ColabGapiDomain: strings.TrimRight(colabGapiDomain, "/"), Client: client, ClientAgent: "craftmake"}
}

func (c *ColabServerClient) token() (string, error) {
	if c.GetAccessToken == nil {
		return "", nil
	}
	return c.GetAccessToken()
}

// Assign obtains a runtime. It GETs the assignment target; if the response
// carries an XSRF token the client must POST to claim the machine.
func (c *ColabServerClient) Assign(ctx context.Context, spec RuntimeSpec) (Assignment, error) {
	path := c.tunPath("assign")
	if spec.NotebookHash == "" {
		return Assignment{}, fmt.Errorf("notebook hash is required")
	}
	path += "?nbh=" + url.QueryEscape(spec.NotebookHash)
	variant := spec.Variant
	if variant == "" {
		variant = "DEFAULT"
	}
	path += "&variant=" + url.QueryEscape(variant)
	if spec.Accelerator != "" {
		path += "&acc=" + url.QueryEscape(spec.Accelerator)
	}
	path += fmt.Sprintf("&shape=%d", spec.Shape)
	if spec.Version != "" {
		path += "&version=" + url.QueryEscape(spec.Version)
	}
	var tokenResponse struct {
		Token string `json:"token"`
	}
	if err := c.do(ctx, http.MethodGet, c.ColabDomain, path, nil, &tokenResponse); err != nil {
		return Assignment{}, err
	}
	if tokenResponse.Token == "" {
		var assigned Assignment
		if err := c.do(ctx, http.MethodGet, c.ColabDomain, path, nil, &assigned); err != nil {
			return Assignment{}, err
		}
		if assigned.Endpoint == "" {
			return Assignment{}, &RemoteError{Kind: ErrorProtocolMismatch, Operation: "assign runtime", Err: fmt.Errorf("assignment response missing endpoint")}
		}
		return assigned, nil
	}
	var posted Assignment
	if err := c.do(ctx, http.MethodPost, c.ColabDomain, path, nil, &posted, tokenResponse.Token); err != nil {
		return Assignment{}, err
	}
	if posted.Endpoint == "" {
		return Assignment{}, &RemoteError{Kind: ErrorProtocolMismatch, Operation: "post assignment", Err: fmt.Errorf("assignment response missing endpoint")}
	}
	return posted, nil
}

// Unassign releases a runtime, following the GET-then-POST XSRF choreography.
func (c *ColabServerClient) Unassign(ctx context.Context, endpoint string) error {
	path := c.tunPath("unassign/" + endpoint)
	var tokenResponse struct {
		Token string `json:"token"`
	}
	if err := c.do(ctx, http.MethodGet, c.ColabDomain, path, nil, &tokenResponse); err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, c.ColabDomain, path, nil, nil, tokenResponse.Token)
}

// KeepAlive sends a keep-alive ping for the assigned endpoint.
func (c *ColabServerClient) KeepAlive(ctx context.Context, endpoint string) error {
	path := c.tunPath(endpoint + "/keep-alive/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.ColabDomain+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set(HeaderTunnel, "Google")
	response, err := c.roundTrip(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return response.Body.Close()
}

// RefreshProxy obtains a fresh runtime proxy token for the endpoint.
func (c *ColabServerClient) RefreshProxy(ctx context.Context, endpoint string) (RuntimeProxyInfo, error) {
	path := "/v1/runtime-proxy-token?endpoint=" + url.QueryEscape(endpoint) + "&port=8080"
	var token RuntimeProxyInfo
	if err := c.do(ctx, http.MethodGet, c.ColabGapiDomain, path, nil, &token); err != nil {
		return RuntimeProxyInfo{}, err
	}
	if token.Token == "" || token.URL == "" {
		return RuntimeProxyInfo{}, &RemoteError{Kind: ErrorProtocolMismatch, Operation: "refresh proxy", Err: fmt.Errorf("proxy token response missing token or url")}
	}
	return token, nil
}

// GetUserInfo returns the current user's tier and accelerator eligibility.
func (c *ColabServerClient) GetUserInfo(ctx context.Context) (UserInfo, error) {
	var info UserInfo
	if err := c.do(ctx, http.MethodGet, c.ColabGapiDomain, "/v1/user-info", nil, &info); err != nil {
		return UserInfo{}, err
	}
	return info, nil
}

func (c *ColabServerClient) tunPath(suffix string) string {
	return TunEndpoint + "/" + strings.TrimLeft(suffix, "/")
}

func (c *ColabServerClient) do(ctx context.Context, method, base, path string, payload any, result any, xsrf ...string) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(HeaderClientAgent, c.ClientAgent)
	if token, err := c.token(); err != nil {
		return &RemoteError{Kind: ErrorAuthRequired, Operation: "refresh access token", Err: err}
	} else if token != "" {
		req.Header.Set(HeaderAuthorization, "Bearer "+token)
	}
	if len(xsrf) > 0 && xsrf[0] != "" {
		req.Header.Set(HeaderXSRF, xsrf[0])
	}
	response, err := c.roundTrip(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if result == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(result); err != nil {
		return &RemoteError{Kind: ErrorProtocolMismatch, Operation: "decode Colab response", Err: err}
	}
	return nil
}

func (c *ColabServerClient) roundTrip(req *http.Request) (*http.Response, error) {
	response, err := c.Client.Do(req)
	if err != nil {
		return nil, &RemoteError{Kind: ErrorKernelDisconnected, Operation: "Colab request", Err: err}
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		if c.OnAuthError != nil {
			_ = c.OnAuthError()
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		response.Body.Close()
		return nil, &RemoteError{Kind: ClassifyHTTPStatus(response.StatusCode), Operation: req.Method + " " + req.URL.Path, StatusCode: response.StatusCode, Err: fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(b)))}
	}
	// Body stays open on success; the caller decodes and closes it.
	return response, nil
}
