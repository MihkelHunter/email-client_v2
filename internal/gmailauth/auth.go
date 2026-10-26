// Package gmailauth handles the Google OAuth login and builds a Gmail client.
package gmailauth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

var ErrNotLoggedIn = errors.New("not logged in: use \"Log in with Google\" on the Account tab")

func LoadConfig(credentialsPath string) (*oauth2.Config, error) {
	b, err := os.ReadFile(credentialsPath)
	if err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	cfg, err := google.ConfigFromJSON(b, gmail.GmailSendScope)
	if err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	return cfg, nil
}

func LoadToken(path string) (*oauth2.Token, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotLoggedIn
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	if err := json.NewDecoder(f).Decode(tok); err != nil {
		return nil, fmt.Errorf("read token: %w", err)
	}
	return tok, nil
}

func SaveToken(path string, tok *oauth2.Token) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

const callbackPage = `<!doctype html><html><head><meta charset="utf-8"><title>%s</title></head>
<body style="font-family:sans-serif;padding:2em"><h2>%s</h2><p>%s</p></body></html>`

// Login runs the OAuth "loopback" flow: it starts a temporary server on
// 127.0.0.1, opens the consent page in the browser and waits for Google to
// redirect back with the authorization code. Requires a "Desktop app" OAuth
// client in credentials.json.
func Login(ctx context.Context, cfg *oauth2.Config, openBrowser func(url string) error) (*oauth2.Token, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start login listener: %w", err)
	}
	c := *cfg
	c.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d/", ln.Addr().(*net.TCPAddr).Port)

	state := rand.Text()
	verifier := oauth2.GenerateVerifier()
	authURL := c.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "consent"),
	)

	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			q := r.URL.Query()
			var res result
			switch {
			case q.Get("state") != state:
				res.err = errors.New("login failed: state mismatch")
			case q.Get("error") != "":
				res.err = fmt.Errorf("login was not authorized: %s", q.Get("error"))
			case q.Get("code") == "":
				res.err = errors.New("login failed: no authorization code received")
			default:
				res.code = q.Get("code")
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if res.err != nil {
				fmt.Fprintf(w, callbackPage, "Login failed", "Login failed", html.EscapeString(res.err.Error()))
			} else {
				fmt.Fprintf(w, callbackPage, "Logged in", "Logged in", "You can close this tab and return to Email Client.")
			}
			select {
			case results <- res:
			default:
			}
		}),
	}
	go srv.Serve(ln)
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()

	if err := openBrowser(authURL); err != nil {
		return nil, fmt.Errorf("open browser: %w", err)
	}

	select {
	case res := <-results:
		if res.err != nil {
			return nil, res.err
		}
		tok, err := c.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
		if err != nil {
			return nil, fmt.Errorf("exchange authorization code: %w", err)
		}
		return tok, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// NewService builds a Gmail client from the saved token. Refreshed tokens are
// written back to tokenPath.
func NewService(ctx context.Context, cfg *oauth2.Config, tokenPath string) (*gmail.Service, error) {
	tok, err := LoadToken(tokenPath)
	if err != nil {
		return nil, err
	}
	ts := &savingTokenSource{
		base: oauth2.ReuseTokenSource(tok, cfg.TokenSource(ctx, tok)),
		path: tokenPath,
		last: tok.AccessToken,
	}
	return gmail.NewService(ctx, option.WithTokenSource(ts))
}

type savingTokenSource struct {
	base oauth2.TokenSource
	path string

	mu   sync.Mutex
	last string
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	t, err := s.base.Token()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.AccessToken != s.last {
		s.last = t.AccessToken
		_ = SaveToken(s.path, t)
	}
	return t, nil
}
