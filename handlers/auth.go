package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"github.com/marvinrabe/goatcounter/internal/httpx"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
)

type AuthMode string

const (
	AuthPublic AuthMode = "public"
	AuthBasic  AuthMode = "basic"
	AuthOIDC   AuthMode = "oidc"
)

type Auth struct {
	Mode       AuthMode
	BasicUsers map[string][]byte
	OIDC       *OIDCAuth
}

type OIDCConfig struct {
	Issuer, ClientID, ClientSecret, RedirectURL, SessionSecret, BasePath string
	Scopes                                                               []string
}

type OIDCAuth struct {
	oauth2   oauth2.Config
	verifier *oidc.IDTokenVerifier
	secret   []byte
	basePath string
}

type oidcState struct {
	State, Nonce, Verifier, Return string
	Expiry                         int64
}

type oidcSession struct {
	Subject string
	Expiry  int64
}

const (
	oidcStateCookie   = "goatcounter_oidc_state"
	oidcSessionCookie = "goatcounter_oidc_session"
)

func NewOIDCAuth(ctx context.Context, cfg OIDCConfig) (*OIDCAuth, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, fmt.Errorf("OIDC issuer, client ID, and client secret are required")
	}
	if err := ValidateOIDCRedirectURL(cfg.RedirectURL); err != nil {
		return nil, err
	}
	if len(cfg.SessionSecret) < 32 {
		return nil, fmt.Errorf("OIDC session secret must contain at least 32 bytes")
	}
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	return &OIDCAuth{
		oauth2: oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret,
			Endpoint: provider.Endpoint(), RedirectURL: cfg.RedirectURL,
			Scopes: uniqueStrings(append([]string{oidc.ScopeOpenID}, cfg.Scopes...)),
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		secret:   []byte(cfg.SessionSecret), basePath: cfg.BasePath,
	}, nil
}

func uniqueStrings(in []string) []string {
	out, seen := make([]string, 0, len(in)), make(map[string]bool, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func (a Auth) Mount(r chi.Router) {
	if a.Mode == AuthOIDC && a.OIDC != nil {
		r.Get("/auth/callback", a.OIDC.callback)
		r.Get("/auth/logout", a.OIDC.logout)
	}
}

func (a Auth) Middleware(next http.Handler) http.Handler {
	switch a.Mode {
	case AuthPublic:
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	case AuthOIDC:
		return a.OIDC.middleware(next)
	default:
		return a.basic(next)
	}
}

func (a Auth) basic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parseForm(r)
		username, password, ok := r.BasicAuth()
		expected, exists := a.BasicUsers[username]
		if ok && exists && bcrypt.CompareHashAndPassword(expected, []byte(password)) == nil {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="GoatCounter"`)
		httpx.ErrPage(w, r, httpx.Error(http.StatusUnauthorized, "Authentication required"))
	})
}

func parseForm(r *http.Request) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		_ = r.ParseMultipartForm(32 << 20)
	} else {
		_ = r.ParseForm()
	}
}

func (a *OIDCAuth) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parseForm(r)
		var session oidcSession
		if c, err := r.Cookie(oidcSessionCookie); err == nil && a.decode(c.Value, &session) &&
			session.Expiry > time.Now().Unix() && session.Subject != "" {
			next.ServeHTTP(w, r)
			return
		}

		state, err := randomToken()
		if err != nil {
			httpx.ErrPage(w, r, err)
			return
		}
		nonce, err := randomToken()
		if err != nil {
			httpx.ErrPage(w, r, err)
			return
		}
		verifier := oauth2.GenerateVerifier()
		returnTo := r.URL.RequestURI()
		if !strings.HasPrefix(returnTo, "/") || strings.HasPrefix(returnTo, "//") {
			returnTo = a.basePath + "/"
		}
		value, err := a.encode(oidcState{
			State: state, Nonce: nonce, Verifier: verifier, Return: returnTo,
			Expiry: time.Now().Add(10 * time.Minute).Unix(),
		})
		if err != nil {
			httpx.ErrPage(w, r, err)
			return
		}
		http.SetCookie(w, a.cookie(r, oidcStateCookie, value, 10*time.Minute))
		http.Redirect(w, r, a.oauth2.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
	})
}

func (a *OIDCAuth) callback(w http.ResponseWriter, r *http.Request) {
	if msg := r.URL.Query().Get("error"); msg != "" {
		httpx.ErrPage(w, r, httpx.Error(http.StatusUnauthorized, "OIDC login failed: "+msg))
		return
	}
	c, err := r.Cookie(oidcStateCookie)
	var state oidcState
	if err != nil || !a.decode(c.Value, &state) || state.Expiry <= time.Now().Unix() ||
		!hmac.Equal([]byte(state.State), []byte(r.URL.Query().Get("state"))) {
		httpx.ErrPage(w, r, httpx.Error(http.StatusBadRequest, "Invalid or expired OIDC state"))
		return
	}
	token, err := a.oauth2.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(state.Verifier))
	if err != nil {
		httpx.ErrPage(w, r, fmt.Errorf("exchange OIDC code: %w", err))
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		httpx.ErrPage(w, r, httpx.Error(http.StatusUnauthorized, "OIDC provider did not return an ID token"))
		return
	}
	idToken, err := a.verifier.Verify(r.Context(), rawIDToken)
	if err != nil || idToken.Nonce != state.Nonce {
		httpx.ErrPage(w, r, httpx.Error(http.StatusUnauthorized, "Invalid OIDC ID token"))
		return
	}
	expires := idToken.Expiry
	if expires.After(time.Now().Add(24 * time.Hour)) {
		expires = time.Now().Add(24 * time.Hour)
	}
	value, err := a.encode(oidcSession{idToken.Subject, expires.Unix()})
	if err != nil {
		httpx.ErrPage(w, r, err)
		return
	}
	http.SetCookie(w, a.cookie(r, oidcSessionCookie, value, time.Until(expires)))
	http.SetCookie(w, a.cookie(r, oidcStateCookie, "", -time.Hour))
	http.Redirect(w, r, state.Return, http.StatusFound)
}

func (a *OIDCAuth) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, a.cookie(r, oidcSessionCookie, "", -time.Hour))
	http.Redirect(w, r, a.basePath+"/", http.StatusFound)
}

func (a *OIDCAuth) cookie(r *http.Request, name, value string, age time.Duration) *http.Cookie {
	path := a.basePath
	if path == "" {
		path = "/"
	}
	return &http.Cookie{Name: name, Value: value, Path: path, HttpOnly: true,
		Secure:   httpx.IsSecure(r),
		SameSite: http.SameSiteLaxMode, MaxAge: int(age.Seconds())}
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("create authentication token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (a *OIDCAuth) encode(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode authentication cookie: %w", err)
	}
	data := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write([]byte(data))
	return data + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (a *OIDCAuth) decode(value string, dst any) bool {
	data, signature, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write([]byte(data))
	want, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(want, mac.Sum(nil)) {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(data)
	return err == nil && json.Unmarshal(b, dst) == nil
}

func ParseBasicUsers(value string) (map[string][]byte, error) {
	users := make(map[string][]byte)
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		username, password, ok := strings.Cut(entry, ":")
		username = strings.TrimSpace(username)
		if !ok || username == "" || password == "" {
			return nil, fmt.Errorf("invalid basic-auth user %q; expected username:password", entry)
		}
		if _, exists := users[username]; exists {
			return nil, fmt.Errorf("duplicate basic-auth user %q", username)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("hash password for basic-auth user %q: %w", username, err)
		}
		users[username] = hash
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("basic authentication requires at least one user")
	}
	return users, nil
}

func ValidateOIDCRedirectURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("OIDC redirect URL must be an absolute http(s) URL")
	}
	if !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/auth/callback") {
		return fmt.Errorf("OIDC redirect URL must end in /auth/callback")
	}
	return nil
}
