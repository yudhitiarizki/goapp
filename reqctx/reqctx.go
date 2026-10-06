// Package reqctx carries request-scoped data (request id, caller identity, IP,
// raw headers) from the HTTP layer down into services through context.Context,
// so a service method like Create(ctx, req) can read it without depending on gin.
package reqctx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/gin-gonic/gin"
)

// HeaderRequestID is the header used to receive and echo the request id.
const HeaderRequestID = "X-Request-ID"

// GinKey is the gin.Context key under which the request id is stored, so the
// gin logger formatter can read it from LogFormatterParams.Keys.
const GinKey = "requestID"

// ApiHeader is the request-scoped data available to every layer. It is stored
// in the context as a pointer, so middlewares running later (e.g. JWT auth)
// can fill in fields such as UserID.
type ApiHeader struct {
	RequestID string         // correlates logs and the response body/header
	UserID    string         // caller id, populated from a valid JWT (empty if anonymous)
	IP        string         // client IP (respects proxy headers via gin ClientIP)
	UserAgent string         // User-Agent header
	Claims    map[string]any // raw JWT claims, nil when there is no valid token
	Raw       http.Header    // all request headers, for anything not promoted above
}

// Get returns a single request header value, or "" if absent.
func (h *ApiHeader) Get(key string) string {
	if h == nil || h.Raw == nil {
		return ""
	}
	return h.Raw.Get(key)
}

type ctxKey struct{}

// NewContext returns a copy of ctx carrying h.
func NewContext(ctx context.Context, h *ApiHeader) context.Context {
	return context.WithValue(ctx, ctxKey{}, h)
}

// FromContext returns the ApiHeader stored in ctx, or a non-nil empty one when
// absent, so callers never have to nil-check.
func FromContext(ctx context.Context) *ApiHeader {
	if h, ok := ctx.Value(ctxKey{}).(*ApiHeader); ok && h != nil {
		return h
	}
	return &ApiHeader{}
}

// Middleware builds the ApiHeader for each request and injects it into the
// request context (so c.Request.Context() carries it into services). It reuses
// an inbound X-Request-ID header when present, otherwise generates one, stores
// the id on the gin context for the logger, and echoes it in the response.
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderRequestID)
		if id == "" {
			id = newRequestID()
		}
		h := &ApiHeader{
			RequestID: id,
			IP:        c.ClientIP(),
			UserAgent: c.Request.UserAgent(),
			Raw:       c.Request.Header,
		}
		c.Request = c.Request.WithContext(NewContext(c.Request.Context(), h))
		c.Set(GinKey, id)
		c.Header(HeaderRequestID, id)
		c.Next()
	}
}

// newRequestID returns a random 32-char hex string.
func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}
