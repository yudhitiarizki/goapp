// Package httpx is a small JSON HTTP client built on the shared *http.Client.
// The library provides the underlying *http.Client (with request-id
// forwarding and a timeout); an adapter wraps it with its own base URL and
// default headers, so every call it makes is pre-configured:
//
//	func NewGateway(hc *http.Client) Gateway {
//	    c := httpx.New(hc,
//	        httpx.BaseURL(os.Getenv("PAYMENT_BASE_URL")),
//	        httpx.Header("Authorization", "Bearer "+os.Getenv("PAYMENT_API_KEY")),
//	    )
//	    return &gateway{c: c}
//	}
//
// Default headers set on the adapter are applied to every request; the shared
// client's transport still forwards X-Request-ID on top of that.
package httpx

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

	"github.com/yudhitiarizki/goapp/errs"
	"github.com/yudhitiarizki/goapp/logging"
)

// Client is a configured JSON HTTP client. Build it with New and reuse it.
type Client struct {
	base    *http.Client
	baseURL string
	headers http.Header
	log     *logging.Logger
	name    string
}

// Option configures a Client.
type Option func(*Client)

// WithLogger makes the client emit a type=adaptor log for every call, tagged
// with name (the adapter/gateway name, e.g. "payment").
func WithLogger(l *logging.Logger, name string) Option {
	return func(c *Client) { c.log, c.name = l, name }
}

// BaseURL sets the prefix prepended to every request Path (trailing slash
// trimmed). A request Path that is already absolute (http/https) is used as-is.
func BaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// Header sets a default header applied to every request. Call it once per
// header; a per-request header with the same key overrides it.
func Header(key, value string) Option {
	return func(c *Client) { c.headers.Set(key, value) }
}

// New wraps the shared *http.Client. When base is nil, http.DefaultClient is
// used (you lose the request-id transport and timeout, so prefer injecting the
// library's client).
func New(base *http.Client, opts ...Option) *Client {
	c := &Client{base: base, headers: make(http.Header)}
	if c.base == nil {
		c.base = http.DefaultClient
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Request is a single JSON call. Body, when non-nil, is JSON-encoded; Out, when
// non-nil, receives the JSON-decoded response body on a 2xx status.
type Request struct {
	Method  string
	Path    string
	Body    any
	Out     any
	Query   url.Values
	Headers map[string]string // per-call headers, override the Client defaults
}

// StatusError is returned when the server responds with a non-2xx status. It
// carries the status code and a (truncated) body for diagnostics.
type StatusError struct {
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("httpx: unexpected status %d: %s", e.StatusCode, e.Body)
}

// Do sends r and decodes the response into r.Out. It returns a *StatusError for
// non-2xx responses so callers can branch on the status.
func (c *Client) Do(ctx context.Context, r Request) (err error) {
	start := time.Now()
	status := 0
	if c.log != nil {
		defer func() {
			attrs := []slog.Attr{
				slog.String("adaptor", c.name),
				slog.String("method", r.Method),
				slog.String("url", c.url(r.Path, r.Query)),
				slog.Int("respHttpStatus", status), // downstream HTTP status (0 if the call never reached the server)
				slog.Int64("latency_ms", time.Since(start).Milliseconds()),
			}
			if err != nil {
				attrs = append(attrs, slog.String("error", err.Error()))
			}
			c.log.Adaptor(ctx, r.Path, attrs...)
		}()
	}

	var reader io.Reader
	if r.Body != nil {
		buf, mErr := json.Marshal(r.Body)
		if mErr != nil {
			err = errs.Wrap(fmt.Errorf("httpx: encode body: %w", mErr))
			return err
		}
		reader = bytes.NewReader(buf)
	}

	req, rErr := http.NewRequestWithContext(ctx, r.Method, c.url(r.Path, r.Query), reader)
	if rErr != nil {
		err = errs.Wrap(fmt.Errorf("httpx: build request: %w", rErr))
		return err
	}

	// defaults first, then per-call overrides.
	for k, vs := range c.headers {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}
	if r.Body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, doErr := c.base.Do(req)
	if doErr != nil {
		err = errs.Wrap(fmt.Errorf("httpx: request failed: %w", doErr))
		return err
	}
	defer resp.Body.Close()
	status = resp.StatusCode

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		err = errs.Wrap(&StatusError{StatusCode: resp.StatusCode, Body: string(body)})
		return err
	}

	if r.Out != nil {
		if dErr := json.NewDecoder(resp.Body).Decode(r.Out); dErr != nil {
			err = errs.Wrap(fmt.Errorf("httpx: decode response: %w", dErr))
			return err
		}
	}
	return nil
}

// GetJSON is a shorthand for a GET whose JSON response decodes into out.
func (c *Client) GetJSON(ctx context.Context, path string, out any) error {
	return c.Do(ctx, Request{Method: http.MethodGet, Path: path, Out: out})
}

// PostJSON is a shorthand for a POST with a JSON body and JSON response.
func (c *Client) PostJSON(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, Request{Method: http.MethodPost, Path: path, Body: body, Out: out})
}

func (c *Client) url(path string, query url.Values) string {
	full := path
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		full = c.baseURL + "/" + strings.TrimLeft(path, "/")
	}
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	return full
}
