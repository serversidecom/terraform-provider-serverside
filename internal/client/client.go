// Package client is a small hand-written client for the Serverside.com API.
//
// Every JSON response is wrapped in a ServiceResponse envelope
// ({success, message, code, errors, traceId, data, pagination}); the client
// unwraps data and turns error responses into *APIError.
package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.serverside.com"

// Client talks to the API with one organization API key.
type Client struct {
	baseURL    *url.URL
	apiKey     string
	userAgent  string
	httpClient *http.Client
	// maxRetries applies to GETs and to requests carrying an Idempotency-Key.
	maxRetries int
	// sleep is replaceable in tests.
	sleep func(context.Context, time.Duration) error
}

type Option func(*Client)

func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }
func WithUserAgent(ua string) Option       { return func(c *Client) { c.userAgent = ua } }
func WithMaxRetries(n int) Option          { return func(c *Client) { c.maxRetries = n } }

func New(baseURL, apiKey string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, errors.New("an API key is required")
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid API URL %q", baseURL)
	}
	c := &Client{
		baseURL:    u,
		apiKey:     apiKey,
		userAgent:  "terraform-provider-serverside",
		httpClient: &http.Client{Timeout: 60 * time.Second},
		maxRetries: 3,
		sleep:      sleepCtx,
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// Pagination mirrors the envelope's pagination block.
type Pagination struct {
	TotalRecords int `json:"totalRecords"`
	CurrentPage  int `json:"currentPage"`
	PageSize     int `json:"pageSize"`
	TotalPages   int `json:"totalPages"`
}

type envelope struct {
	Success    bool                `json:"success"`
	Message    string              `json:"message"`
	Code       string              `json:"code"`
	Errors     map[string][]string `json:"errors"`
	TraceID    string              `json:"traceId"`
	Data       json.RawMessage     `json:"data"`
	Pagination *Pagination         `json:"pagination"`
}

// APIError is a non-2xx response.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	TraceID    string
	Errors     map[string][]string
	Method     string
	Path       string
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s: HTTP %d", e.Method, e.Path, e.StatusCode)
	if e.Code != "" {
		fmt.Fprintf(&b, " %s", e.Code)
	}
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	for field, msgs := range e.Errors {
		fmt.Fprintf(&b, "; %s: %s", field, strings.Join(msgs, ", "))
	}
	if e.TraceID != "" {
		fmt.Fprintf(&b, " (traceId %s)", e.TraceID)
	}
	return b.String()
}

// IsNotFound reports whether err is a 404 from the API.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// HasCode reports whether err is an API error with the given error code.
func HasCode(err error, code string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == code
}

type request struct {
	method         string
	path           string
	query          url.Values
	body           any
	idempotencyKey string
}

// do sends one request and decodes the envelope's data into out (when out is
// non-nil). It returns the status code and the pagination block, if any.
func (c *Client) do(ctx context.Context, r request, out any) (int, *Pagination, error) {
	var payload []byte
	if r.body != nil {
		var err error
		payload, err = json.Marshal(r.body)
		if err != nil {
			return 0, nil, fmt.Errorf("encode request body: %w", err)
		}
	}

	u := *c.baseURL
	u.Path = c.baseURL.Path + r.path
	if len(r.query) > 0 {
		u.RawQuery = r.query.Encode()
	}

	retryable := r.method == http.MethodGet || r.idempotencyKey != ""
	attempts := 1
	if retryable {
		attempts += c.maxRetries
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			wait := backoff(attempt, lastErr)
			if err := c.sleep(ctx, wait); err != nil {
				return 0, nil, err
			}
		}

		req, err := http.NewRequestWithContext(ctx, r.method, u.String(), bytes.NewReader(payload))
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("X-API-KEY", c.apiKey)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.userAgent)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if r.idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", r.idempotencyKey)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return 0, nil, ctx.Err()
			}
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		if resp.StatusCode >= 400 {
			apiErr := decodeError(resp, body, r)
			if retryable && isRetryableStatus(resp.StatusCode) {
				lastErr = &retryAfterError{err: apiErr, after: retryAfter(resp)}
				continue
			}
			return resp.StatusCode, nil, apiErr
		}

		if len(bytes.TrimSpace(body)) == 0 {
			return resp.StatusCode, nil, nil
		}
		var env envelope
		if err := json.Unmarshal(body, &env); err != nil {
			return resp.StatusCode, nil, fmt.Errorf("%s %s: decode response: %w", r.method, r.path, err)
		}
		if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
			if err := json.Unmarshal(env.Data, out); err != nil {
				return resp.StatusCode, nil, fmt.Errorf("%s %s: decode data: %w", r.method, r.path, err)
			}
		}
		return resp.StatusCode, env.Pagination, nil
	}

	var ra *retryAfterError
	if errors.As(lastErr, &ra) {
		return 0, nil, ra.err
	}
	return 0, nil, fmt.Errorf("%s %s: %w", r.method, r.path, lastErr)
}

func decodeError(resp *http.Response, body []byte, r request) *APIError {
	apiErr := &APIError{StatusCode: resp.StatusCode, Method: r.method, Path: r.path}
	// Most errors use the envelope; a few (403 SERVICE_NOT_AVAILABLE) do not,
	// so decode leniently and fall back to the raw text.
	var env envelope
	if err := json.Unmarshal(body, &env); err == nil {
		apiErr.Code = env.Code
		apiErr.Message = env.Message
		apiErr.TraceID = env.TraceID
		apiErr.Errors = env.Errors
	}
	if apiErr.Code == "" && apiErr.Message == "" {
		apiErr.Message = strings.TrimSpace(string(body))
		if len(apiErr.Message) > 300 {
			apiErr.Message = apiErr.Message[:300] + "..."
		}
	}
	return apiErr
}

type retryAfterError struct {
	err   *APIError
	after time.Duration
}

func (e *retryAfterError) Error() string { return e.err.Error() }

func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func retryAfter(resp *http.Response) time.Duration {
	if s := resp.Header.Get("Retry-After"); s != "" {
		if secs, err := strconv.Atoi(s); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 0
}

func backoff(attempt int, lastErr error) time.Duration {
	var ra *retryAfterError
	if errors.As(lastErr, &ra) && ra.after > 0 {
		return ra.after
	}
	return time.Duration(attempt*attempt) * time.Second
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// NewIdempotencyKey returns a random key for one create action. Terraform does
// not keep it between runs, so it protects retries inside one request only.
func NewIdempotencyKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "tf-" + hex.EncodeToString(b)
}

const pageSize = 100

// listAll pages through a list endpoint.
func listAll[T any](ctx context.Context, c *Client, path string, query url.Values) ([]T, error) {
	if query == nil {
		query = url.Values{}
	}
	var all []T
	for page := 1; ; page++ {
		q := url.Values{}
		for k, v := range query {
			q[k] = v
		}
		q.Set("page", strconv.Itoa(page))
		q.Set("page_size", strconv.Itoa(pageSize))
		var items []T
		_, pg, err := c.do(ctx, request{method: http.MethodGet, path: path, query: q}, &items)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if pg == nil || pg.TotalPages <= page || len(items) == 0 {
			return all, nil
		}
	}
}

func pathEscape(s string) string { return url.PathEscape(s) }
