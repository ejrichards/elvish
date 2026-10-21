package doc

import (
	_ "embed"
	"fmt"
	"strings"

	"src.elv.sh/pkg/elvdoc"
	"src.elv.sh/pkg/md"
)

// Shared with the website through its @include macro.
//
//go:embed language.md
var languageSource string

// extractLanguage makes each section of the language reference available under
// its website anchor, including its subsections. Parsing Markdown rather than
// scanning for '#' avoids treating comments in code examples as headings.
func extractLanguage(source string) elvdoc.Docs {
	var codec languageHeadingCodec
	md.Render(source, &codec)
	headings := codec.headings
	lines := strings.SplitAfter(source, "\n")
	offsets := make([]int, len(lines)+1)
	for i, line := range lines {
		offsets[i+1] = offsets[i] + len(line)
	}
	var index strings.Builder
	index.WriteString("# Language reference\n\n")
	index.WriteString("Use `doc:show language:<topic>` to read a section, or\n")
	index.WriteString("`doc:source language:<topic>` to get its Markdown source.\n\n")
	var docs elvdoc.Docs
	for i, heading := range headings {
		var text md.TextCodec
		text.Do(heading)
		title := text.Blocks()[0].Text
		id := ""
		for _, attr := range strings.Fields(heading.Info) {
			if value, ok := strings.CutPrefix(attr, "#"); ok {
				id = value
				break
			}
		}
		if id == "" {
			id = strings.Join(strings.Fields(strings.ToLower(title)), "-")
		}
		end := len(source)
		for _, next := range headings[i+1:] {
			if next.Number <= heading.Number {
				end = offsets[next.LineNo-1]
				break
			}
		}
		name := "language:" + id
		docs.Fns = append(docs.Fns, elvdoc.Entry{
			Name: name, Content: source[offsets[heading.LineNo-1]:end], LineNo: heading.LineNo,
		})
		fmt.Fprintf(&index, "%s- `%s`: %s\n", strings.Repeat("  ", heading.Number-1), name, title)
	}
	docs.File = &elvdoc.FileEntry{Content: index.String()}
	return docs
}

type languageHeadingCodec struct{ headings []md.Op }

func (c *languageHeadingCodec) Do(op md.Op) {
	if op.Type == md.OpHeading {
		c.headings = append(c.headings, op)
	}
}
