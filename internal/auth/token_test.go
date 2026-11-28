package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTokenOrder(t *testing.T) {
	for _, c := range []struct {
		name, header, query, cookie, want string
	}{
		{"header first", "Bearer h", "q", "c", "h"},
		// An app's image loader sends an old session cookie along with
		// the token it was asked to use: the token wins.
		{"query before cookie", "", "q", "c", "q"},
		{"cookie alone", "", "", "c", "c"},
		{"nothing", "", "", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			url := "/api/v1/items/1/image/poster"
			if c.query != "" {
				url += "?token=" + c.query
			}
			r := httptest.NewRequest(http.MethodGet, url, nil)
			if c.header != "" {
				r.Header.Set("Authorization", c.header)
			}
			if c.cookie != "" {
				r.AddCookie(&http.Cookie{Name: CookieName, Value: c.cookie})
			}
			if got := Token(r); got != c.want {
				t.Fatalf("Token = %q, want %q", got, c.want)
			}
		})
	}
}
