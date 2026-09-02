// Package textconv converts rich content into plain text suitable for an LLM.
package textconv

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	// paragraphTags get a newline on open and close, yielding blank-line separation.
	paragraphTags = map[string]bool{"p": true, "div": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "blockquote": true, "pre": true, "table": true, "ul": true, "ol": true, "section": true, "article": true, "header": true, "footer": true}
	// lineTags get a single newline on open only.
	lineTags = map[string]bool{"br": true, "li": true, "tr": true, "hr": true}
	skipTags = map[string]bool{"script": true, "style": true, "head": true, "title": true, "noscript": true}

	whitespaceRun = regexp.MustCompile(`\s+`)
	spaceRun      = regexp.MustCompile(`[ \t\r\f\v]+`)
	newlineRun    = regexp.MustCompile(`\n{3,}`)
	trailingWS    = regexp.MustCompile(`[ \t]+\n`)
	leadingWSNL   = regexp.MustCompile(`\n[ \t]+`)
)

// HTMLToText strips markup, keeping block structure as newlines.
func HTMLToText(src string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(src))
	var sb strings.Builder
	skipDepth := 0

	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return normalize(sb.String())
		case html.StartTagToken, html.SelfClosingTagToken:
			tag := tagName(tokenizer)
			switch {
			case skipTags[tag]:
				skipDepth++
			case paragraphTags[tag], lineTags[tag]:
				sb.WriteByte('\n')
			}
		case html.EndTagToken:
			tag := tagName(tokenizer)
			switch {
			case skipTags[tag] && skipDepth > 0:
				skipDepth--
			case paragraphTags[tag]:
				sb.WriteByte('\n')
			}
		case html.TextToken:
			if skipDepth > 0 {
				continue
			}
			sb.WriteString(whitespaceRun.ReplaceAllString(html.UnescapeString(string(tokenizer.Text())), " "))
		}
	}
}

func tagName(t *html.Tokenizer) string {
	name, _ := t.TagName()
	return string(name)
}

func normalize(s string) string {
	s = spaceRun.ReplaceAllString(s, " ")
	s = trailingWS.ReplaceAllString(s, "\n")
	s = leadingWSNL.ReplaceAllString(s, "\n")
	s = newlineRun.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
