package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
)

func TestParseBasicUsers(t *testing.T) {
	users, err := ParseBasicUsers("alice:one,bob:two:with-colon")
	if err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword(users["alice"], []byte("one")) != nil ||
		bcrypt.CompareHashAndPassword(users["bob"], []byte("two:with-colon")) != nil {
		t.Fatalf("passwords were not hashed correctly: %#v", users)
	}
	for _, value := range []string{"", "missing-password:", ":missing-username", "same:one,same:two"} {
		if _, err := ParseBasicUsers(value); err == nil {
			t.Errorf("ParseBasicUsers(%q) unexpectedly succeeded", value)
		}
	}
}

func TestAuthMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	public := Auth{Mode: AuthPublic}.Middleware(next)
	rr := httptest.NewRecorder()
	public.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("public status: %d", rr.Code)
	}

	users, err := ParseBasicUsers("alice:secret")
	if err != nil {
		t.Fatal(err)
	}
	basic := Auth{Mode: AuthBasic, BasicUsers: users}.Middleware(next)
	for _, tc := range []struct {
		user, password string
		want           int
	}{{"alice", "secret", 204}, {"alice", "wrong", 401}, {"unknown", "secret", 401}} {
		r := httptest.NewRequest("GET", "/", nil)
		r.SetBasicAuth(tc.user, tc.password)
		rr = httptest.NewRecorder()
		basic.ServeHTTP(rr, r)
		if rr.Code != tc.want {
			t.Errorf("%s/%s: got %d; want %d", tc.user, tc.password, rr.Code, tc.want)
		}
	}
}

func TestOIDCStartsLogin(t *testing.T) {
	a := &OIDCAuth{
		oauth2: oauth2.Config{ClientID: "client", Endpoint: oauth2.Endpoint{AuthURL: "https://id.example/auth"}},
		secret: []byte("01234567890123456789012345678901"),
	}
	rr := httptest.NewRecorder()
	a.middleware(http.NotFoundHandler()).ServeHTTP(rr, httptest.NewRequest("GET", "/?site=example.com", nil))
	if rr.Code != http.StatusFound || !strings.HasPrefix(rr.Header().Get("Location"), "https://id.example/auth?") {
		t.Fatalf("unexpected redirect: %d %q", rr.Code, rr.Header().Get("Location"))
	}
	if len(rr.Result().Cookies()) == 0 || rr.Result().Cookies()[0].Name != oidcStateCookie {
		t.Fatal("OIDC state cookie was not set")
	}
}

func TestOIDCCookieSigning(t *testing.T) {
	a := &OIDCAuth{secret: []byte("01234567890123456789012345678901")}
	want := oidcSession{"subject", time.Now().Add(time.Hour).Unix()}
	value, err := a.encode(want)
	if err != nil {
		t.Fatal(err)
	}
	var have oidcSession
	if !a.decode(value, &have) || have != want {
		t.Fatalf("decoded %#v; want %#v", have, want)
	}
	if a.decode(value+"tampered", &have) {
		t.Fatal("accepted tampered cookie")
	}
}

func TestValidateOIDCRedirectURL(t *testing.T) {
	if err := ValidateOIDCRedirectURL("https://stats.example/auth/callback"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"/auth/callback", "https://stats.example/other"} {
		if err := ValidateOIDCRedirectURL(value); err == nil {
			t.Errorf("ValidateOIDCRedirectURL(%q) unexpectedly succeeded", value)
		}
	}
}
