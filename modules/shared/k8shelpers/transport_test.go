package k8shelpers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
)

// Note: The InClusterSATRoundTripper tests were written by codex. They're not bad
//       but could be more readable. The next person to edit them should do some
//       refactoring.

var TEST_TOKEN = "eyJhbGciOiJSUzI1NiIsImtpZCI6IjEyMzQ1NiJ9.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwic3ViIjoic3lzdGVtOnNvbWUtbmFtZXNwYWNlOnRlc3QtdXNlciIsImF1ZCI6Imt1YmVybmV0ZXMiLCJleHAiOjQ3Mjg5MjQ4MDAsImlhdCI6MTY1MDkzMjAwMH0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"

func TestBearerTokenRoundTripper_headerSet(t *testing.T) {
	testserver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header, ok := r.Header["Authorization"]
		token := header[0]

		assert.True(t, ok)
		assert.Equal(t, "Bearer "+TEST_TOKEN, token)
	}))

	b := NewBearerTokenRoundTripper(http.DefaultTransport)

	c := &http.Client{Transport: b}

	req, err := http.NewRequest("GET", testserver.URL, nil)
	assert.Nil(t, err)

	ctx := context.WithValue(req.Context(), K8STokenCtxKey, TEST_TOKEN)
	req = req.WithContext(ctx)

	_, err = c.Do(req)
	assert.Nil(t, err)
}

func TestInClusterSATRoundTripper_getTokenUsesCache(t *testing.T) {
	rt := &InClusterSATRoundTripper{
		path:        "does-not-exist",
		refreshSkew: time.Minute,
		cachedToken: "cached-token",
		expiresAt:   time.Now().Add(5 * time.Minute),
	}

	token, err := rt.getToken()
	assert.NoError(t, err)
	assert.Equal(t, "cached-token", token)
}

func TestInClusterSATRoundTripper_getTokenRefreshesWhenNearExpiration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	expectedExpiry := time.Now().Add(10 * time.Minute)
	expectedToken := writeTokenFile(t, path, expectedExpiry)

	rt := &InClusterSATRoundTripper{
		path:        path,
		refreshSkew: time.Minute,
		cachedToken: "stale-token",
		expiresAt:   time.Now().Add(30 * time.Second),
	}

	token, err := rt.getToken()
	assert.NoError(t, err)
	assert.Equal(t, expectedToken, token)
	assert.Equal(t, expectedToken, rt.cachedToken)
	assert.WithinDuration(t, expectedExpiry, rt.expiresAt, time.Second)
}

func TestInClusterSATRoundTripper_RoundTripSetsAuthorizationHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	tokenString := writeTokenFile(t, path, time.Now().Add(5*time.Minute))

	var capturedAuth string
	mockTransport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		capturedAuth = req.Header.Get("Authorization")
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	rt := &InClusterSATRoundTripper{
		Transport:   mockTransport,
		path:        path,
		refreshSkew: time.Minute,
	}

	req, err := http.NewRequest("GET", "http://example.com", nil)
	assert.NoError(t, err)

	resp, err := rt.RoundTrip(req)
	assert.NoError(t, err)
	assert.Equal(t, "Bearer "+tokenString, capturedAuth)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestInClusterSATRoundTripper_readTokenFromFileAllowsMissingExp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	tokenString := newTestTokenWithoutExp(t)
	if err := os.WriteFile(path, []byte(tokenString), 0o600); err != nil {
		t.Fatalf("failed to write token file: %v", err)
	}

	rt := &InClusterSATRoundTripper{path: path}
	token, exp, err := rt.readTokenFromFile()
	assert.NoError(t, err)
	assert.Equal(t, tokenString, token)
	assert.True(t, exp.IsZero())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func writeTokenFile(t *testing.T, path string, expiresAt time.Time) string {
	t.Helper()
	token := newTestToken(t, expiresAt)
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		t.Fatalf("failed to write token file: %v", err)
	}

	return token
}

func newTestToken(t *testing.T, expiresAt time.Time) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp": expiresAt.Unix(),
	})

	signed, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	return signed
}

func newTestTokenWithoutExp(t *testing.T) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"aud": "kubernetes",
		"iat": time.Now().Unix(),
	})

	signed, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	return signed
}
