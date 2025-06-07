package websocket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
	csrfProtect.ServeHTTP(rr, r)

	if rr.Code != 200 {
		return errors.New("AUTHORIZATION_REQUIRED")
	}

	return nil
}
