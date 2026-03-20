package highlight

import (
	"bytes"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// Result contains the highlighted HTML and detected language.
type Result struct {
	HTML     string `json:"html"`
	Language string `json:"language"`
}

// Highlight returns syntax-highlighted HTML for the given file content.
func Highlight(filename, content string) Result {
	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Analyse(content)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	lang := lexer.Config().Name

	formatter := html.New(
		html.WithLineNumbers(true),
		html.WithLinkableLineNumbers(true, "L"),
		html.WithClasses(true),
	)

	style := styles.Get("github")
	if style == nil {
		style = styles.Fallback
	}

	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return Result{HTML: content, Language: "plaintext"}
	}

	var buf bytes.Buffer
	err = formatter.Format(&buf, style, iterator)
	if err != nil {
		return Result{HTML: content, Language: "plaintext"}
	}

	return Result{HTML: buf.String(), Language: lang}
}
