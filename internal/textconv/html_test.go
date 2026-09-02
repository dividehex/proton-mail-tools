package textconv

import "testing"

func TestHTMLToText(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"paragraphs", "<p>Hello</p><p>World</p>", "Hello\n\nWorld"},
		{"inline", "<b>bold</b> and <i>italic</i>", "bold and italic"},
		{"skips script and style", "<style>p{}</style><script>x()</script><p>ok</p>", "ok"},
		{"entities", "a &amp; b &lt;c&gt;", "a & b <c>"},
		{"line breaks", "one<br>two<br/>three", "one\ntwo\nthree"},
		{"collapses whitespace", "<div>  lots   of\n\n\n   space </div>", "lots of space"},
		{"list", "<ul><li>a</li><li>b</li></ul>", "a\nb"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HTMLToText(c.in); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}
