package websocket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gorilla/csrf"
)

type ctxKey int

const CookiesCtxKey ctxKey = iota

func ValidateCSRFToken(ctx context.Context, csrfProtect http.Handler, csrfToken string) error {
	cookies, ok := ctx.Value(CookiesCtxKey).([]*http.Cookie)
	if !ok {
		return errors.New("missing cookies")
	}

	// Make mock request
	r, _ := http.NewRequest("POST", "/", nil)
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	r.Header.Set("X-CSRF-Token", csrfToken)

	// Run request through csrf protect function
	rr := httptest.NewRecorder()

	// As this is a mock request, we must signal that the request is being served over plaintext HTTP
	// so Referer-based origin allow-listing checks are skipped
	mockReqContext := context.WithValue(ctx, csrf.PlaintextHTTPContextKey, true)
	csrfProtect.ServeHTTP(rr, r.WithContext(mockReqContext))
	if rr.Code != 200 {
		return errors.New("AUTHORIZATION_REQUIRED")
	}

	return nil
}
