package doc

import (
	"strings"
	"testing"
)

func TestExtractLanguage(t *testing.T) {
	source := "<!-- toc -->\n\n# **First** heading\n\nOverview.\n\n" +
		"## A `child` {#custom-id}\n\n```elvish\n# Not a heading\n```\n\n" +
		"### Grandchild\n\nDetails.\n\n# Last heading\n\nEnd.\n"
	docs := extractLanguage(source)
	wantNames := []string{"language:first-heading", "language:custom-id", "language:grandchild", "language:last-heading"}
	if len(docs.Fns) != len(wantNames) {
		t.Fatalf("got %d topics, want %d", len(docs.Fns), len(wantNames))
	}
	for i, want := range wantNames {
		if got := docs.Fns[i].Name; got != want {
			t.Errorf("topic %d = %q, want %q", i, got, want)
		}
		if !strings.Contains(docs.File.Content, "`"+want+"`") {
			t.Errorf("index missing %q", want)
		}
		if docs.Fns[i].Fn != nil {
			t.Errorf("topic %q has function usage", want)
		}
	}
	wantFirst := source[strings.Index(source, "# **First**"):strings.Index(source, "# Last heading")]
	if got := docs.Fns[0].Content; got != wantFirst {
		t.Errorf("first section = %q, want %q", got, wantFirst)
	}
	wantChild := source[strings.Index(source, "## A"):strings.Index(source, "# Last heading")]
	if got := docs.Fns[1].Content; got != wantChild {
		t.Errorf("child section = %q, want %q", got, wantChild)
	}
	if got, want := docs.Fns[3].Content, "# Last heading\n\nEnd.\n"; got != want {
		t.Errorf("last section = %q, want %q", got, want)
	}
}

func TestLanguageReference(t *testing.T) {
	docs := extractLanguage(languageSource)
	seen := make(map[string]bool)
	for _, entry := range docs.Fns {
		if seen[entry.Name] {
			t.Errorf("duplicate topic %q", entry.Name)
		}
		seen[entry.Name] = true
		if !strings.HasPrefix(entry.Content, "#") {
			t.Errorf("topic %q does not start at its heading", entry.Name)
		}
	}
	for _, name := range []string{
		"language:single-quoted-string", "language:scoping-rule", "language:redirection",
		"language:var", "language:if", "language:for", "language:try", "language:modules",
	} {
		if !seen[name] {
			t.Errorf("missing topic %q", name)
		}
	}
	if seen["language:no-newline-at-all"] {
		t.Error("code comment treated as heading")
	}
}

func TestLanguageSource(t *testing.T) {
	for _, name := range []string{"language", "language:var", "language:redirection"} {
		got, err := Source(name)
		if err != nil {
			t.Errorf("Source(%q): %v", name, err)
		} else if !strings.HasPrefix(got, "#") {
			t.Errorf("Source(%q) = %q, want a Markdown section", name, got)
		}
	}
	for _, name := range []string{"language:", "language:nonexistent"} {
		if _, err := Source(name); err == nil {
			t.Errorf("Source(%q) succeeded", name)
		}
	}
}
