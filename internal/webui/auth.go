package webui

import (
	"crypto/subtle"
	"io"
	"net/http"
	"strings"
)

// tokenCookie is the session cookie set after a successful ?token= login, so
// the token never has to stay in the address bar or browser history.
const tokenCookie = "fftui_token"

// withAuth wraps the handler in token auth. Inactive when no token is
// configured. Accepted channels, in order:
//
//  1. the fftui_token cookie (constant-time compare)
//  2. an Authorization: Bearer <token> header
//  3. a one-shot ?token= query parameter — on success it sets the cookie and
//     303-redirects to the same URL with the parameter STRIPPED
//
// A wrong token via any channel gets a 401 and no cookie. /static/ is exempt
// so the 401 page can load its stylesheet.
func (s *Server) withAuth(next http.Handler) http.Handler {
	if s.opts.Token == "" {
		return next
	}
	want := []byte(s.opts.Token)
	ok := func(got string) bool {
		return subtle.ConstantTimeCompare([]byte(got), want) == 1
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(tokenCookie); err == nil && ok(c.Value) {
			next.ServeHTTP(w, r)
			return
		}
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") && ok(strings.TrimPrefix(h, "Bearer ")) {
			next.ServeHTTP(w, r)
			return
		}
		if q := r.URL.Query(); q.Has("token") && ok(q.Get("token")) {
			// No Secure flag: the expected deployment is plain HTTP on a home
			// LAN. SameSite=Lax keeps the cookie off cross-site subrequests.
			http.SetCookie(w, &http.Cookie{
				Name:     tokenCookie,
				Value:    s.opts.Token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			q.Del("token")
			u := *r.URL
			u.RawQuery = q.Encode()
			http.Redirect(w, r, u.RequestURI(), http.StatusSeeOther)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, unauthorizedPage)
	})
}

// unauthorizedPage deliberately says nothing about what the server hosts.
const unauthorizedPage = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>401</title>
<link rel="stylesheet" href="/static/style.css"></head>
<body><main class="errorpage"><h1>401</h1><p>unauthorized</p></main></body></html>
`
