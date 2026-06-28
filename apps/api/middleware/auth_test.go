package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret"

func signToken(t *testing.T, secret string, expiresAt time.Time) string {
	t.Helper()
	claims := jwt.RegisteredClaims{
		Subject:   "test-user",
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}
	return signed
}

func runJWTMiddleware(req *http.Request) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	r := gin.New()
	r.Use(JWT(testSecret))
	r.GET("/protected", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	r.ServeHTTP(w, req)
	return w
}

func TestJWT_ValidBearerHeader_Allows(t *testing.T) {
	token := signToken(t, testSecret, time.Now().Add(time.Hour))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := runJWTMiddleware(req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestJWT_MissingAuth_Rejects(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := runJWTMiddleware(req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestJWT_MalformedHeader_Rejects(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "NotBearer sometoken")
	w := runJWTMiddleware(req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestJWT_WrongSecret_Rejects(t *testing.T) {
	token := signToken(t, "wrong-secret", time.Now().Add(time.Hour))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := runJWTMiddleware(req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestJWT_ExpiredToken_Rejects(t *testing.T) {
	token := signToken(t, testSecret, time.Now().Add(-time.Hour))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := runJWTMiddleware(req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// TestJWT_QueryParamFallback_Allows covers the WebSocket case: browsers
// cannot set custom headers on the handshake request, so /ws and
// /ws/logs/:container rely on the ?token= fallback in extractToken.
func TestJWT_QueryParamFallback_Allows(t *testing.T) {
	token := signToken(t, testSecret, time.Now().Add(time.Hour))
	req := httptest.NewRequest(http.MethodGet, "/protected?token="+token, nil)
	w := runJWTMiddleware(req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 via query param fallback, got %d: %s", w.Code, w.Body.String())
	}
}

func TestJWT_HeaderTakesPrecedenceOverQueryParam(t *testing.T) {
	validToken := signToken(t, testSecret, time.Now().Add(time.Hour))
	badToken := "garbage"
	req := httptest.NewRequest(http.MethodGet, "/protected?token="+validToken, nil)
	req.Header.Set("Authorization", "Bearer "+badToken)

	w := runJWTMiddleware(req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 (header present but invalid must not fall back to query param), got %d", w.Code)
	}
}
