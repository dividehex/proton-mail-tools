package bridge

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	mimemail "github.com/emersion/go-message/mail"

	"proton-mail-tools/internal/mail"
)

// SMTPSender implements mail.Sender over the bridge's STARTTLS SMTP endpoint.
type SMTPSender struct {
	addr      string
	username  string
	password  string
	tlsConfig *tls.Config
}

// NewSMTPSender configures a sender for the bridge SMTP endpoint.
func NewSMTPSender(addr, username, password string, tlsConfig *tls.Config) *SMTPSender {
	return &SMTPSender{addr: addr, username: username, password: password, tlsConfig: tlsConfig}
}

// Send builds a MIME message and submits it. Bridge files a copy in Sent itself.
func (s *SMTPSender) Send(ctx context.Context, out mail.Outgoing) error {
	raw, err := buildMessage(out)
	if err != nil {
		return fmt.Errorf("build message: %w", err)
	}

	dialer := &net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("connect to bridge SMTP %s: %w", s.addr, err)
	}
	host, _, _ := net.SplitHostPort(s.addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp greeting: %w", err)
	}
	defer c.Close()

	if err := c.StartTLS(s.tlsConfig); err != nil {
		return fmt.Errorf("smtp starttls: %w", err)
	}
	if err := c.Auth(smtp.PlainAuth("", s.username, s.password, host)); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := c.Mail(out.From.Email); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	for _, rcpt := range out.Recipients() {
		if err := c.Rcpt(rcpt.Email); err != nil {
			return fmt.Errorf("smtp RCPT TO %s: %w", rcpt.Email, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp finish DATA: %w", err)
	}
	return c.Quit()
}

// buildMessage serialises out as a quoted-printable text/plain RFC 5322 message.
func buildMessage(out mail.Outgoing) ([]byte, error) {
	var h mimemail.Header
	h.SetDate(time.Now())
	h.SetAddressList("From", toMIMEAddresses([]mail.Address{out.From}))
	h.SetAddressList("To", toMIMEAddresses(out.To))
	if len(out.Cc) > 0 {
		h.SetAddressList("Cc", toMIMEAddresses(out.Cc))
	}
	h.SetSubject(out.Subject)
	if err := h.GenerateMessageIDWithHostname(domainOf(out.From.Email)); err != nil {
		return nil, err
	}
	if out.InReplyTo != "" {
		h.SetMsgIDList("In-Reply-To", []string{out.InReplyTo})
	}
	if len(out.References) > 0 {
		h.SetMsgIDList("References", out.References)
	}
	h.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
	h.Set("Content-Transfer-Encoding", "quoted-printable")

	var buf bytes.Buffer
	w, err := mimemail.CreateSingleInlineWriter(&buf, h)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write([]byte(out.Body)); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func toMIMEAddresses(in []mail.Address) []*mimemail.Address {
	out := make([]*mimemail.Address, 0, len(in))
	for _, a := range in {
		out = append(out, &mimemail.Address{Name: a.Name, Address: a.Email})
	}
	return out
}

func domainOf(email string) string {
	if i := strings.LastIndex(email, "@"); i >= 0 && i < len(email)-1 {
		return email[i+1:]
	}
	return "localhost"
}
