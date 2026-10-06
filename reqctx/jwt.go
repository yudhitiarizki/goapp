package reqctx

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Token kinds, carried in the "typ" claim so an access token and a refresh
// token can be told apart. Auth only accepts access tokens; a refresh token is
// exchanged for a new pair at the refresh endpoint.
const (
	ClaimType   = "typ"
	TypeAccess  = "access"
	TypeRefresh = "refresh"
)

// ErrNotRefreshToken is returned by ParseRefresh when the token is valid but is
// not a refresh token.
var ErrNotRefreshToken = errors.New("reqctx: not a refresh token")

// Signer issues and verifies JWTs with a fixed secret. It knows two lifetimes:
// a short one for access tokens and a long one for refresh tokens. goapp
// provides a *Signer through fx, so a domain can depend on it by type:
//
//	func NewService(repo Repository, signer *reqctx.Signer) Service { ... }
type Signer struct {
	secret     string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewSigner returns a Signer. A zero or negative TTL falls back to 15m for
// access tokens and 7 days for refresh tokens.
func NewSigner(secret string, accessTTL, refreshTTL time.Duration) *Signer {
	if accessTTL <= 0 {
		accessTTL = 15 * time.Minute
	}
	if refreshTTL <= 0 {
		refreshTTL = 7 * 24 * time.Hour
	}
	return &Signer{secret: secret, accessTTL: accessTTL, refreshTTL: refreshTTL}
}

// RefreshTTL reports the configured refresh-token lifetime, so callers can
// store a matching expiry alongside a persisted session.
func (s *Signer) RefreshTTL() time.Duration { return s.refreshTTL }

// Access signs a short-lived access token for userID.
func (s *Signer) Access(userID string, extra map[string]any) (string, error) {
	return s.sign(userID, s.accessTTL, TypeAccess, extra)
}

// Refresh signs a long-lived refresh token for userID. Keep extra minimal; a
// refresh token should carry little more than identity.
func (s *Signer) Refresh(userID string, extra map[string]any) (string, error) {
	return s.sign(userID, s.refreshTTL, TypeRefresh, extra)
}

func (s *Signer) sign(userID string, ttl time.Duration, typ string, extra map[string]any) (string, error) {
	claims := map[string]any{ClaimType: typ}
	for k, v := range extra {
		claims[k] = v
	}
	return GenerateToken(s.secret, userID, ttl, claims)
}

// ParseRefresh verifies a refresh token and returns its subject (userID) and
// claims. It fails with ErrNotRefreshToken if the token is valid but not a
// refresh token, so an access token cannot be replayed at the refresh endpoint.
func (s *Signer) ParseRefresh(token string) (string, map[string]any, error) {
	claims, err := parseToken(s.secret, token)
	if err != nil {
		return "", nil, err
	}
	if t, _ := claims[ClaimType].(string); t != TypeRefresh {
		return "", nil, ErrNotRefreshToken
	}
	return userID(claims), claims, nil
}

// GenerateToken signs an HS256 JWT for userID, valid for ttl. It sets the
// standard sub/iat/exp claims; any entries in extra are merged in (and may
// override them). The resulting token is accepted by Auth with the same secret.
func GenerateToken(secret, userID string, ttl time.Duration, extra map[string]any) (string, error) {
	if secret == "" {
		return "", errors.New("reqctx: empty JWT secret")
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": userID,
		"iat": now.Unix(),
		"exp": now.Add(ttl).Unix(),
	}
	for k, v := range extra {
		claims[k] = v
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// Auth parses a Bearer JWT from the Authorization header. When the token is
// valid it fills ApiHeader.UserID and .Claims; when it is missing or invalid it
// leaves them empty and still calls the next handler, so public endpoints keep
// working. Protect specific routes by adding RequireAuth after this.
//
// It expects the ApiHeader to already be on the context, so register it AFTER
// Middleware().
func Auth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr := bearerToken(c.GetHeader("Authorization"))
		if tokenStr != "" && secret != "" {
			if claims, err := parseToken(secret, tokenStr); err == nil {
				// Refresh tokens must not authenticate requests; only exchange
				// them at the refresh endpoint.
				if t, _ := claims[ClaimType].(string); t != TypeRefresh {
					h := FromContext(c.Request.Context())
					h.UserID = userID(claims)
					h.Claims = claims
				}
			}
		}
		c.Next()
	}
}

// parseToken verifies an HMAC-signed JWT and returns its claims.
func parseToken(secret, token string) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	tok, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	if !tok.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// RequireAuth aborts with 401 when no authenticated user is present. Attach it
// to route groups that must not be reached anonymously.
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if FromContext(c.Request.Context()).UserID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success":   false,
				"requestId": FromContext(c.Request.Context()).RequestID,
				"error":     "unauthorized",
			})
			return
		}
		c.Next()
	}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(header string) string {
	const prefix = "bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	return ""
}

// userID reads the caller id from the standard "sub" claim, falling back to a
// "user_id" claim.
func userID(claims jwt.MapClaims) string {
	if sub, _ := claims["sub"].(string); sub != "" {
		return sub
	}
	if uid, _ := claims["user_id"].(string); uid != "" {
		return uid
	}
	return ""
}
