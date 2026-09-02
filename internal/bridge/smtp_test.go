package bridge

import (
	"strings"
	"testing"

	"proton-mail-tools/internal/mail"
)

func TestBuildMessage(t *testing.T) {
	raw, err := buildMessage(mail.Outgoing{
		From:       mail.Address{Name: "Me", Email: "me@example.com"},
		To:         []mail.Address{{Email: "alice@example.com"}},
		Cc:         []mail.Address{{Name: "Bob", Email: "bob@example.com"}},
		Bcc:        []mail.Address{{Email: "hidden@example.com"}},
		Subject:    "Hi",
		Body:       "héllo world",
		InReplyTo:  "orig@example.com",
		References: []string{"root@example.com", "orig@example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	msg := string(raw)
	for _, want := range []string{
		"From: \"Me\" <me@example.com>",
		"To: <alice@example.com>",
		"Cc: \"Bob\" <bob@example.com>",
		"Subject: Hi",
		"In-Reply-To: <orig@example.com>",
		"References: <root@example.com> <orig@example.com>",
		"Message-Id: <",
		"@example.com>",
		"Content-Type: text/plain; charset=utf-8",
		"h=C3=A9llo world",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "hidden@example.com") {
		t.Errorf("Bcc address must not appear in headers:\n%s", msg)
	}
}
