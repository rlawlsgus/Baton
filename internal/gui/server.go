// Package gui is baton's graphical interface: a small HTTP server on
// 127.0.0.1 that serves an embedded web page, shown in an app window.
//
// The page only reads baton's files through this server and asks it to act.
// Every change to accounts or sessions is made by running baton.exe itself
// (hidden, or in a new console window when the user has to take part, such as
// a sign-in), so the CLI stays the one place that enforces baton's rules.
package gui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"baton/internal/app"
	"baton/internal/update"
)

//go:embed web
var webFS embed.FS

// Spawner runs baton.exe.
type Spawner interface {
	// Console starts baton.exe with args in a new console window (for work the
	// user takes part in: sign-in, a Claude Code session). wait blocks until it
	// ends and returns its exit code.
	Console(dir string, args []string) (wait func() int, err error)
	// Hidden runs baton.exe with args without a window and returns its output.
	Hidden(args []string) (output string, code int, err error)
}

type Options struct {
	Paths   app.Paths
	Spawner Spawner
	Version string
	// Lang is the language Windows is set to ("ko" or "en"); config can override it.
	Lang string
	// Claude reports which Claude Code baton would use (slow: runs --version).
	Claude func() ClaudeInfo
	// PickFolder shows the native folder dialog; "" means cancelled.
	PickFolder func() (string, error)
	// Open shows a folder or file in Explorer.
	Open func(path string) error
	// Clipboard reads the clipboard text; OpenPrivate opens a URL in an
	// InPrivate window (the sign-in for a second account).
	Clipboard   func() string
	OpenPrivate func(url string) error
	// Install copies baton to the user's Programs folder (Start menu entry,
	// installed-apps entry) and returns the installed baton-app.exe;
	// Relaunch opens that one and closes this window; Uninstall opens the
	// uninstall window and closes this one. Installed says where this runs.
	Install   func() (string, error)
	Relaunch  func(app string) error
	Uninstall func() error
	Installed func() (installed, canInstall bool)
	// CheckUpdate asks GitHub for baton's newest release; Update downloads
	// it, checks it against its checksums and installs it like Install,
	// returning the installed baton-app.exe for Relaunch.
	CheckUpdate func(ctx context.Context) (update.Release, error)
	Update      func(ctx context.Context, rel update.Release) (string, error)
	// Focus brings a session's console window (a window handle) to the
	// front; TestNotify shows a sample notification in lang.
	Focus      func(window uintptr) error
	TestNotify func(lang string) error
}

type ClaudeInfo struct {
	Found   bool   `json:"found"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"` // config | native | path | npm | winget | desktop
	Warning string `json:"warning,omitempty"`
	Error   string `json:"error,omitempty"`
	// When Claude Code was not found or cannot be used: whether the desktop
	// app is installed (then its Code tab sets it up), and whether the copy
	// found is too old.
	DesktopApp bool `json:"desktop_app"`
	TooOld     bool `json:"too_old"`
	// BundleOff: the desktop app's Claude Code exists, and using it is
	// turned off in the settings.
	BundleOff bool `json:"bundle_off"`
	// OldCLI: the version of a Claude Code CLI too old to use, passed over
	// for the one found.
	OldCLI string `json:"old_cli,omitempty"`
}

type Server struct {
	opt  Options
	addr string // 127.0.0.1:<port>

	mu         sync.Mutex
	codes      map[string]time.Time        // one-time window codes -> expiry
	tokens     map[string]map[string]*page // session token -> the page loads using it, by page id
	lastSeen   time.Time
	jobs       []*job
	nextJob    int
	checking   bool
	recheck    bool // a check was asked for while one was running
	cache      readCache
	claude     ClaudeInfo
	claudeAt   time.Time
	claudeBusy bool
	changes    map[string]*changesEntry // git changes of running sessions, by run id
	upd        updateState
}

// page is one load of the page in a window or browser tab. A reload, or a
// duplicated tab (which copies the token), is a new load with its own id.
type page struct {
	lastSeen time.Time
	byeAt    time.Time // the page said it was closing
	byeSeq   int       // the number of that request
}

// pageHeader carries a random id the page picks each time it loads, and
// seqHeader numbers its requests: a poll sent before the page said bye, but
// answered after it, must not count as the page coming back.
const (
	pageHeader = "X-Baton-Page"
	seqHeader  = "X-Baton-Seq"
)

const codeTTL = 2 * time.Minute

// tokenHeader carries the page's session token. Unlike a cookie, which a
// browser sends to every 127.0.0.1 port, it goes only where the page sends
// it, and a page from another site cannot add it without a CORS preflight
// (never answered).
const tokenHeader = "X-Baton-Token"

func New(opt Options) *Server {
	return &Server{
		opt:      opt,
		codes:    map[string]time.Time{},
		tokens:   map[string]map[string]*page{},
		lastSeen: time.Now(),
	}
}

// Listen binds a random port on 127.0.0.1.
func (s *Server) Listen() (net.Listener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s.addr = ln.Addr().String()
	return ln, nil
}

// Origin is the page's origin, e.g. http://127.0.0.1:51234.
func (s *Server) Origin() string { return "http://" + s.addr }

// WindowURL returns a URL that opens the GUI once: it carries a one-time code
// that the page exchanges for a session token, after which it is useless.
func (s *Server) WindowURL() string {
	code := randomHex(32)
	s.mu.Lock()
	s.codes[code] = time.Now().Add(codeTTL)
	s.mu.Unlock()
	return s.Origin() + "/?k=" + code
}

// Idle is how long no page has talked to the server.
func (s *Server) Idle() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.lastSeen)
}

// Closed reports that every opened page said it was closing and has not
// talked to the server for 3 s since (a reload also says it is closing, then
// comes back).
func (s *Server) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, loads := range s.tokens {
		for _, p := range loads {
			n++
			if p.byeAt.IsZero() || p.lastSeen.After(p.byeAt) || time.Since(p.byeAt) <= 3*time.Second {
				return false
			}
		}
	}
	return n > 0
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; "+
		"connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'; "+
		"require-trusted-types-for 'script'; trusted-types 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")

	// A name that resolves to 127.0.0.1 (DNS rebinding) must not reach the API.
	if r.Host != s.addr {
		http.Error(w, "wrong host", http.StatusMisdirectedRequest)
		return
	}
	// The page and its script hold nothing secret; the API needs the session
	// token the page gets for the window URL's one-time code.
	if strings.HasPrefix(r.URL.Path, "/api/") {
		s.serveAPI(w, r)
		return
	}
	s.serveStatic(w, r)
}

// apiSession exchanges a window URL's one-time code for a session token.
func (s *Server) apiSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.sameOrigin(r) {
		fail(w, http.StatusForbidden, "forbidden", "forbidden", "")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	now := time.Now()
	s.mu.Lock()
	exp, ok := s.codes[req.Code]
	delete(s.codes, req.Code)
	for c, e := range s.codes {
		if now.After(e) {
			delete(s.codes, c)
		}
	}
	var token string
	if ok && now.Before(exp) {
		token = randomHex(32)
		s.tokens[token] = map[string]*page{}
		s.lastSeen = now
	}
	s.mu.Unlock()
	if token == "" {
		fail(w, http.StatusForbidden, "expired", "this link has expired; open baton again", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// authed finds the page load a request comes from (nil: no valid token).
func (s *Server) authed(r *http.Request) *page {
	t := r.Header.Get(tokenHeader)
	if t == "" {
		return nil
	}
	id := r.Header.Get(pageHeader)
	if len(id) > 64 {
		id = id[:64]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, loads := range s.tokens {
		if subtle.ConstantTimeCompare([]byte(token), []byte(t)) != 1 {
			continue
		}
		p := loads[id]
		if p == nil {
			if len(loads) >= 64 { // reloads leave closed loads behind
				for k, old := range loads {
					if !old.byeAt.IsZero() {
						delete(loads, k)
					}
				}
			}
			p = &page{}
			loads[id] = p
		}
		seq, _ := strconv.Atoi(r.Header.Get(seqHeader))
		if p.byeAt.IsZero() || seq > p.byeSeq { // later than its bye: it is back (bfcache)
			p.lastSeen = time.Now()
		}
		s.lastSeen = time.Now()
		return p
	}
	return nil
}

// sameOrigin rejects requests that browsers mark as coming from another site.
func (s *Server) sameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" && o != s.Origin() {
		return false
	}
	if f := r.Header.Get("Sec-Fetch-Site"); f != "" && f != "same-origin" {
		return false
	}
	return true
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	types := map[string]string{
		"index.html": "text/html; charset=utf-8",
		"app.js":     "text/javascript; charset=utf-8",
		"app.css":    "text/css; charset=utf-8",
		"icon.svg":   "image/svg+xml",
	}
	ct, ok := types[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(webFS, "web/"+name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	_, _ = w.Write(data)
}

// ---- helpers -----------------------------------------------------------------

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`    // machine-readable reason for the page
	Details string `json:"details,omitempty"` // baton.exe output
}

func fail(w http.ResponseWriter, status int, code, msg, details string) {
	writeJSON(w, status, apiError{Error: msg, Code: code, Details: details})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "bad_request", "bad request: "+err.Error(), "")
		return false
	}
	return true
}

var errNoSpawner = errors.New("baton.exe is not available next to baton-app.exe")
