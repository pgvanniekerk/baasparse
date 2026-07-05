package httpserver

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/auth"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

const sessionCookie = "baasparse_session"

const (
	sessionIdle     = 30 * time.Minute
	sessionAbsolute = 12 * time.Hour
)

type ctxKey int

const userCtxKey ctxKey = 0

// currentUser returns the authenticated user from the request context, or nil.
func currentUser(r *http.Request) *store.User {
	if u, ok := r.Context().Value(userCtxKey).(*store.User); ok {
		return u
	}
	return nil
}

// authMiddleware enforces a valid session for all but the public routes,
// injecting the user into the request context (BR-USR-002, BR-UI-010).
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublic(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		u, ok := s.userFromRequest(r)
		if !ok {
			// HTML navigation → redirect to login; anything else → 401.
			if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
				http.Redirect(w, r, "/login?next="+r.URL.Path, http.StatusSeeOther)
				return
			}
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isPublic(path string) bool {
	switch path {
	case "/login", "/logout", "/healthz", "/readyz", "/metrics":
		return true
	}
	return strings.HasPrefix(path, "/static/")
}

// userFromRequest validates the session cookie and returns the user, sliding the
// idle expiry forward on success.
func (s *Server) userFromRequest(r *http.Request) (*store.User, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil, false
	}
	hash := auth.HashToken(c.Value)
	u, err := s.store.SessionUser(r.Context(), hash)
	if err != nil {
		return nil, false
	}
	_ = s.store.TouchSession(r.Context(), hash, time.Now().Add(sessionIdle))
	return &u, true
}

// --- login / logout ---

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.userFromRequest(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, "login", map[string]any{"Next": r.URL.Query().Get("next")})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	next := r.FormValue("next")

	fail := func() {
		w.WriteHeader(http.StatusUnauthorized)
		s.render(w, "login", map[string]any{"Error": "Invalid username or password.", "Username": username, "Next": next})
	}

	u, err := s.store.GetUserByUsername(r.Context(), username)
	if err != nil || u.Status != "ACTIVE" || !auth.VerifyPassword(password, u.PasswordHash) {
		s.log.Warn("login failed", "username", username)
		fail()
		return
	}

	token, err := auth.GenerateSessionToken()
	if err != nil {
		s.fail(w, err)
		return
	}
	now := time.Now()
	if err := s.store.CreateSession(r.Context(), u.UID, auth.HashToken(token), now.Add(sessionIdle), now.Add(sessionAbsolute), clientAddr(r)); err != nil {
		s.fail(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil, // alpha runs over http; TLS is BR-NFR-053
		Expires:  now.Add(sessionAbsolute),
	})
	s.log.Info("login", "username", u.Username)
	if next == "" || !strings.HasPrefix(next, "/") {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		_ = s.store.RevokeSession(r.Context(), auth.HashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func clientAddr(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	return r.RemoteAddr
}
