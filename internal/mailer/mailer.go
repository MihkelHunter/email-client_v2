// Package mailer builds MIME messages and sends them through Gmail.
package mailer

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/net/idna"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

// NormalizeAddress validates an address ("a@b.ee" or "Name <a@b.ee>") and
// returns it as a header value with the domain converted to ASCII (IDNA).
func NormalizeAddress(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	a, err := mail.ParseAddress(addr)
	if err != nil {
		return "", fmt.Errorf("invalid email address %q", addr)
	}
	at := strings.LastIndex(a.Address, "@")
	domain, err := idna.Lookup.ToASCII(a.Address[at+1:])
	if err != nil {
		return "", fmt.Errorf("invalid email domain in %q", addr)
	}
	a.Address = a.Address[:at+1] + domain
	return a.String(), nil
}

type Message struct {
	From    string
	To      string
	Subject string
	HTML    string
}

// Bytes renders the message as RFC 5322 text with a quoted-printable HTML body.
func (m Message) Bytes() ([]byte, error) {
	from, err := NormalizeAddress(m.From)
	if err != nil {
		return nil, fmt.Errorf("sender: %w", err)
	}
	to, err := NormalizeAddress(m.To)
	if err != nil {
		return nil, err
	}
	subject := strings.NewReplacer("\r", " ", "\n", " ").Replace(m.Subject)

	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")

	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(m.HTML)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

type Sender interface {
	Send(ctx context.Context, raw []byte) error
}

// GmailSender sends raw messages with the Gmail API, retrying on rate limits
// and transient server errors.
type GmailSender struct {
	Service *gmail.Service
}

const maxAttempts = 4

func (g GmailSender) Send(ctx context.Context, raw []byte) error {
	msg := &gmail.Message{Raw: base64.URLEncoding.EncodeToString(raw)}
	backoff := 2 * time.Second
	for attempt := 1; ; attempt++ {
		_, err := g.Service.Users.Messages.Send("me", msg).Context(ctx).Do()
		if err == nil || attempt == maxAttempts || !retryable(err) {
			return err
		}
		select {
		case <-time.After(backoff):
			backoff *= 2
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func retryable(err error) bool {
	var gerr *googleapi.Error
	if !errors.As(err, &gerr) {
		return false
	}
	if gerr.Code == 429 || gerr.Code >= 500 {
		return true
	}
	for _, e := range gerr.Errors {
		if e.Reason == "rateLimitExceeded" || e.Reason == "userRateLimitExceeded" {
			return true
		}
	}
	return false
}
