package textconv

import "testing"

func TestExtractLinks(t *testing.T) {
	src := `<p>Hi <a href="https://example.com/a">first  link</a> and <a href="#top">anchor</a>
	<a href="javascript:void(0)">js</a> <a href="https://example.com/a">dup</a>
	<a href="mailto:unsub@example.com?subject=x">Unsubscribe</a> <a href="https://example.com/b"><b>bold</b> b</a></p>`
	got := ExtractLinks(src, 10)
	want := []Link{
		{"first link", "https://example.com/a"},
		{"Unsubscribe", "mailto:unsub@example.com?subject=x"},
		{"bold b", "https://example.com/b"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("link %d: got %+v want %+v", i, got[i], want[i])
		}
	}
	if n := len(ExtractLinks(src, 1)); n != 1 {
		t.Errorf("max should cap results, got %d", n)
	}
}
