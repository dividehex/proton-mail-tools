package textconv

import (
	"strings"

	"golang.org/x/net/html"
)

// Link is a hyperlink found in HTML content.
type Link struct {
	Text string
	URL  string
}

// ExtractLinks returns the distinct http(s)/mailto links in src, in document
// order, with their visible anchor text. At most max links are returned.
func ExtractLinks(src string, max int) []Link {
	tokenizer := html.NewTokenizer(strings.NewReader(src))
	var (
		out     []Link
		seen    = map[string]bool{}
		current *Link
		text    strings.Builder
	)
	flush := func() {
		if current == nil {
			return
		}
		current.Text = strings.Join(strings.Fields(text.String()), " ")
		out = append(out, *current)
		current, text = nil, strings.Builder{}
	}
	for len(out) < max {
		switch tokenizer.Next() {
		case html.ErrorToken:
			flush()
			return out
		case html.StartTagToken:
			name, hasAttr := tokenizer.TagName()
			if string(name) != "a" || !hasAttr {
				continue
			}
			flush()
			if href := attr(tokenizer, "href"); usableLink(href) && !seen[href] {
				seen[href] = true
				current = &Link{URL: href}
			}
		case html.EndTagToken:
			if name, _ := tokenizer.TagName(); string(name) == "a" {
				flush()
			}
		case html.TextToken:
			if current != nil {
				text.WriteString(html.UnescapeString(string(tokenizer.Text())))
			}
		}
	}
	return out
}

func attr(t *html.Tokenizer, want string) string {
	for {
		key, val, more := t.TagAttr()
		if string(key) == want {
			return strings.TrimSpace(string(val))
		}
		if !more {
			return ""
		}
	}
}

func usableLink(href string) bool {
	lower := strings.ToLower(href)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:")
}
