package k8shelpers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
