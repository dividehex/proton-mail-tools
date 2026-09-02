package bridge

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // register legacy charsets
	mimemail "github.com/emersion/go-message/mail"

	"proton-mail-tools/internal/mail"
	"proton-mail-tools/internal/textconv"
)

// maxLinks caps the hyperlinks reported per message; newsletters can carry hundreds.
const maxLinks = 50

// parseBody extracts the best plain-text body, References, List-Unsubscribe,
// hyperlinks and attachment metadata from a raw RFC 5322 message into msg.
func parseBody(raw []byte, msg *mail.Message) error {
	msg.Attachments = []mail.Attachment{}
	msg.Links = []mail.Link{}

	mr, err := mimemail.CreateReader(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) {
		return err
	}
	if refs, err := mr.Header.MsgIDList("References"); err == nil {
		msg.References = refs
	}
	msg.ListUnsubscribe = parseListUnsubscribe(mr.Header.Get("List-Unsubscribe"))

	var text, html string
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if message.IsUnknownCharset(err) {
				continue
			}
			return err
		}
		switch h := part.Header.(type) {
		case *mimemail.InlineHeader:
			ct, _, _ := h.ContentType()
			body, _ := io.ReadAll(part.Body)
			switch {
			case ct == "text/plain" && text == "":
				text = string(body)
			case ct == "text/html" && html == "":
				html = string(body)
			}
		case *mimemail.AttachmentHeader:
			name, _ := h.Filename()
			ct, _, _ := h.ContentType()
			size, _ := io.Copy(io.Discard, part.Body)
			msg.Attachments = append(msg.Attachments, mail.Attachment{Filename: name, ContentType: ct, Size: size})
		}
	}

	if text != "" {
		msg.Body = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	} else {
		msg.Body = textconv.HTMLToText(html)
	}
	for _, l := range textconv.ExtractLinks(html, maxLinks) {
		msg.Links = append(msg.Links, mail.Link{Text: l.Text, URL: l.URL})
	}
	msg.HasAttachments = len(msg.Attachments) > 0
	return nil
}

// parseListUnsubscribe splits an RFC 2369 header value ("<url>, <mailto:...>") into URLs.
func parseListUnsubscribe(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		if u := strings.Trim(strings.TrimSpace(part), "<>"); u != "" {
			out = append(out, u)
		}
	}
	return out
}
