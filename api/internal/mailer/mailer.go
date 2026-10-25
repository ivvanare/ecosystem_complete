// Package mailer delivers email over SMTP. In development the target is Mailpit, which
// accepts everything and shows it in a local web UI.
package mailer

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// Message is one plain-text email.
type Message struct {
	To      string
	Subject string
	Text    string
}

// SMTP sends messages to a plain SMTP server. It deliberately has no TLS or auth path:
// the development target is Mailpit on the compose network. A real provider needs
// STARTTLS and credentials, which is a separate change.
type SMTP struct {
	Addr    string
	From    string
	Timeout time.Duration
}

// New returns a sender with a bounded dial timeout.
func New(addr, from string) *SMTP {
	return &SMTP{Addr: addr, From: from, Timeout: 5 * time.Second}
}

// Send delivers the message, honouring the context deadline.
func (s *SMTP) Send(ctx context.Context, message Message) error {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	host, _, err := net.SplitHostPort(s.Addr)
	if err != nil {
		return fmt.Errorf("mailer: bad SMTP address %q: %w", s.Addr, err)
	}

	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("mailer: dialing SMTP: %w", err)
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("mailer: SMTP handshake: %w", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Mail(s.From); err != nil {
		return fmt.Errorf("mailer: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(message.To); err != nil {
		return fmt.Errorf("mailer: RCPT TO: %w", err)
	}

	body, err := client.Data()
	if err != nil {
		return fmt.Errorf("mailer: DATA: %w", err)
	}
	if _, err := body.Write([]byte(render(s.From, message))); err != nil {
		_ = body.Close()
		return fmt.Errorf("mailer: writing body: %w", err)
	}
	if err := body.Close(); err != nil {
		return fmt.Errorf("mailer: closing body: %w", err)
	}

	return client.Quit()
}

// render builds a minimal RFC 5322 message. Header injection is not a concern for the
// subjects this service sends, but the values are still sanitised of newlines.
func render(from string, message Message) string {
	var b strings.Builder
	b.WriteString("From: " + sanitize(from) + "\r\n")
	b.WriteString("To: " + sanitize(message.To) + "\r\n")
	b.WriteString("Subject: " + sanitize(message.Subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(message.Text)
	return b.String()
}

func sanitize(value string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(value)
}
