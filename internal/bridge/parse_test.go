package bridge

import (
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"proton-mail-tools/internal/mail"
)

const multipartMsg = "From: Alice <alice@example.com>\r\n" +
	"To: me@example.com\r\n" +
	"Subject: Test\r\n" +
	"References: <root@example.com> <mid@example.com>\r\n" +
	"List-Unsubscribe: <https://example.com/unsub?u=1>, <mailto:unsub@example.com>\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=\"outer\"\r\n\r\n" +
	"--outer\r\nContent-Type: multipart/alternative; boundary=\"inner\"\r\n\r\n" +
	"--inner\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nplain body\r\n" +
	"--inner\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>html body <a href=\"https://example.com/x\">Click</a></p>\r\n" +
	"--inner--\r\n" +
	"--outer\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"doc.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERi0=\r\n" +
	"--outer--\r\n"

func TestParseBodyPrefersPlainTextAndListsAttachments(t *testing.T) {
	var msg mail.Message
	if err := parseBody([]byte(multipartMsg), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Body != "plain body" {
		t.Fatalf("body = %q", msg.Body)
	}
	if len(msg.References) != 2 || msg.References[0] != "root@example.com" {
		t.Fatalf("references = %v", msg.References)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].Filename != "doc.pdf" || msg.Attachments[0].ContentType != "application/pdf" || msg.Attachments[0].Size != 5 {
		t.Fatalf("attachments = %+v", msg.Attachments)
	}
	if !msg.HasAttachments {
		t.Fatal("expected HasAttachments")
	}
	if len(msg.ListUnsubscribe) != 2 || msg.ListUnsubscribe[0] != "https://example.com/unsub?u=1" || msg.ListUnsubscribe[1] != "mailto:unsub@example.com" {
		t.Fatalf("list_unsubscribe = %v", msg.ListUnsubscribe)
	}
	if len(msg.Links) != 1 || msg.Links[0] != (mail.Link{Text: "Click", URL: "https://example.com/x"}) {
		t.Fatalf("links = %+v (links must come from the HTML part even when text/plain is used as body)", msg.Links)
	}
}

func TestParseBodyFallsBackToHTML(t *testing.T) {
	raw := "Subject: x\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<div>Hi <b>there</b></div><p>bye</p>"
	var msg mail.Message
	if err := parseBody([]byte(raw), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Body != "Hi there\n\nbye" {
		t.Fatalf("body = %q", msg.Body)
	}
	if msg.Attachments == nil || len(msg.Attachments) != 0 || msg.Links == nil || len(msg.Links) != 0 {
		t.Fatalf("attachments and links should be empty slices, got %#v %#v", msg.Attachments, msg.Links)
	}
}

func TestRoleOf(t *testing.T) {
	if got := roleOf("INBOX", nil); got != mail.RoleInbox {
		t.Fatalf("INBOX role = %q", got)
	}
	if got := roleOf("Folders/Work", nil); got != "" {
		t.Fatalf("custom folder role = %q", got)
	}
	if got := roleOf("Whatever", []imap.MailboxAttr{imap.MailboxAttrTrash}); got != mail.RoleTrash {
		t.Fatalf("special-use trash role = %q", got)
	}
}

func TestPickNewest(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dated := []datedUID{
		{uid: 1, date: base.AddDate(0, 8, 0)}, // newest, lowest UID (Bridge sync order)
		{uid: 2, date: base.AddDate(0, 2, 0)},
		{uid: 3, date: base.AddDate(0, 5, 0)},
		{uid: 4, date: base},
	}
	got := pickNewest(dated, 2)
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("expected [1 3], got %v", got)
	}
}
