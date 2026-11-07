package layout

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"

	"github.com/a-h/templ"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Step text and author-note text are markdown, rendered to a safe subset
// (ADR-00046). The subset is fixed by what the parser is built from, not by
// cleaning the output: the parser knows paragraphs, lists, blockquotes,
// emphasis, inline code and links, and nothing else, so a heading, a code
// block, a horizontal rule, a table or raw HTML is never parsed as one and
// stays in its paragraph as text. Images, links to anything but http and
// https, and autolinks likewise turn back into the text they were written as.
// The HTML writer escapes all text, so the source cannot write markup of its
// own. A single newline breaks the line.

// mdParser is built from the allowed block and inline parsers only. Left out:
// headings (ATX and setext), thematic breaks, indented and fenced code, HTML
// blocks, raw inline HTML, and the link reference definition transformer, so
// "[x]: http://..." stays a visible line instead of vanishing.
var mdParser = parser.NewParser(
	parser.WithBlockParsers(
		util.Prioritized(noRuleLists{parser.NewListParser()}, 300),
		util.Prioritized(parser.NewListItemParser(), 400),
		util.Prioritized(parser.NewBlockquoteParser(), 800),
		util.Prioritized(indentedParagraphs{parser.NewParagraphParser()}, 1000),
	),
	parser.WithInlineParsers(
		util.Prioritized(noFenceCode{parser.NewCodeSpanParser()}, 100),
		util.Prioritized(sourceTextFilter{parser.NewLinkParser()}, 200),
		util.Prioritized(sourceTextFilter{parser.NewAutoLinkParser()}, 300),
		util.Prioritized(parser.NewEmphasisParser(), 500),
	),
)

// mdRenderer is goldmark's HTML renderer, raw HTML off, with hard wraps on and
// mdNodes in front of it for links and for the kinds the parser never makes.
var mdRenderer = renderer.NewRenderer(renderer.WithNodeRenderers(
	util.Prioritized(html.NewRenderer(html.WithHardWraps()), 1000),
	util.Prioritized(mdNodes{}, 100),
))

// linkRel is the rel of every link a recipe's text carries (ADR-00046).
const linkRel = "nofollow noopener noreferrer"

// RenderMarkdown renders step or author-note text as HTML in the subset of
// ADR-00046. It is the one renderer of that text: the recipe page, a past
// version and cook mode all show it through Markdown.
func RenderMarkdown(src string) (string, error) {
	source := []byte(src)
	var buf bytes.Buffer
	if err := mdRenderer.Render(&buf, source, mdParser.Parse(text.NewReader(source))); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return buf.String(), nil
}

// Markdown is RenderMarkdown as a component. It writes the paragraphs and
// lists alone; the caller's wrapper carries the class that styles them. A
// render error fails the page rather than showing the text some other way.
func Markdown(src string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		out, err := RenderMarkdown(src)
		if err != nil {
			return err
		}
		// The renderer's output is the one place step text is trusted as HTML.
		return templ.Raw(out).Render(ctx, w)
	})
}

// noRuleLists is goldmark's list parser refusing a line that is a horizontal
// rule. With no thematic break parser, "* * *" or "- - -" would otherwise open
// three empty lists nested in each other; refused, it stays a line of text.
type noRuleLists struct {
	parser.BlockParser
}

func (p noRuleLists) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	if line, _ := reader.PeekLine(); horizontalRule(line) {
		return nil, parser.NoChildren
	}
	return p.BlockParser.Open(parent, reader, pc)
}

// horizontalRule reports whether a line is a CommonMark thematic break: up to
// three spaces, then three or more of one of "*", "-" or "_", with nothing but
// spaces and tabs between them.
func horizontalRule(line []byte) bool {
	line = bytes.TrimRight(line, " \t\r\n")
	trimmed := bytes.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) == 0 {
		return false
	}
	mark := trimmed[0]
	if mark != '*' && mark != '-' && mark != '_' {
		return false
	}
	n := 0
	for _, c := range trimmed {
		switch c {
		case mark:
			n++
		case ' ', '\t':
		default:
			return false
		}
	}
	return n >= 3
}

// indentedParagraphs is goldmark's paragraph parser taking lines indented four
// spaces or more. Those belong to the indented code block parser, which is
// left out, and with no parser taking them the line would vanish from the
// page. As a paragraph it shows, its indent trimmed.
type indentedParagraphs struct {
	parser.BlockParser
}

func (indentedParagraphs) CanAcceptIndentedLine() bool { return true }

// noFenceCode is goldmark's code span parser leaving a run of three or more
// backticks as text, so a fenced code block stays its source lines instead of
// becoming one span of inline code. Inline code written with one or two
// backticks parses as usual.
type noFenceCode struct {
	parser.InlineParser
}

func (p noFenceCode) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, seg := block.PeekLine()
	run := 0
	for run < len(line) && line[run] == '`' {
		run++
	}
	if run >= 3 {
		block.Advance(run)
		return ast.NewTextSegment(seg.WithStop(seg.Start + run))
	}
	return p.InlineParser.Parse(parent, block, pc)
}

// sourceTextFilter wraps goldmark's link or autolink parser and turns what it
// parses back into the source text when the subset does not allow it: every
// image, and a link or autolink whose destination is not http or https.
type sourceTextFilter struct {
	parser.InlineParser
}

// Parse runs the wrapped parser. A refused node becomes a text segment over
// exactly the characters it was parsed from, which the writer escapes.
func (f sourceTextFilter) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	_, before := block.PeekLine()
	n := f.InlineParser.Parse(parent, block, pc)
	start := 0
	switch v := n.(type) {
	case *ast.Image:
		start = v.Pos() // the "!" of "![", set by the link parser
	case *ast.Link:
		if webURL(v.Destination) {
			return n
		}
		start = v.Pos() // the "[" that opened the label
	case *ast.AutoLink:
		if v.AutoLinkType == ast.AutoLinkURL && webURL(v.URL(block.Source())) {
			return n
		}
		start = before.Start // the "<"
	default:
		return n
	}
	_, after := block.Position()
	return ast.NewTextSegment(text.NewSegment(start, after.Start))
}

// CloseBlock passes on to the link parser, which turns brackets left open at
// the end of a paragraph back into text.
func (f sourceTextFilter) CloseBlock(parent ast.Node, block text.Reader, pc parser.Context) {
	if cb, ok := f.InlineParser.(parser.CloseBlocker); ok {
		cb.CloseBlock(parent, block, pc)
	}
}

// webURL reports whether a destination is an absolute http or https URL.
func webURL(dest []byte) bool {
	u, err := url.Parse(string(dest))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// mdNodes renders links with their rel, and refuses every node kind the
// parser is built never to make. Reaching one means the parser was changed to
// allow it, and the page fails rather than show markup the subset forbids.
type mdNodes struct{}

func (mdNodes) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindLink, renderLink)
	reg.Register(ast.KindAutoLink, renderAutoLink)
	for _, k := range []ast.NodeKind{
		ast.KindImage, ast.KindRawHTML, ast.KindHTMLBlock, ast.KindHeading,
		ast.KindCodeBlock, ast.KindFencedCodeBlock, ast.KindThematicBreak,
		ast.KindLinkReferenceDefinition,
	} {
		reg.Register(k, refuse)
	}
}

func refuse(_ util.BufWriter, _ []byte, n ast.Node, _ bool) (ast.WalkStatus, error) {
	return ast.WalkStop, fmt.Errorf("markdown node %s is outside the allowed subset", n.Kind())
}

func renderLink(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Link)
	if !entering {
		_, _ = w.WriteString("</a>")
		return ast.WalkContinue, nil
	}
	if err := openLink(w, n.Destination, n.Title); err != nil {
		return ast.WalkStop, err
	}
	return ast.WalkContinue, nil
}

func renderAutoLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.AutoLink)
	if !entering {
		return ast.WalkContinue, nil
	}
	if n.AutoLinkType != ast.AutoLinkURL {
		return ast.WalkStop, fmt.Errorf("markdown email autolink is outside the allowed subset")
	}
	if err := openLink(w, n.URL(source), nil); err != nil {
		return ast.WalkStop, err
	}
	_, _ = w.Write(util.EscapeHTML(n.Label(source)))
	_, _ = w.WriteString("</a>")
	return ast.WalkContinue, nil
}

// openLink writes an anchor's start tag. The parser already turned every
// other destination into text; checking again keeps a parser change from
// reaching the page as a javascript: link. A nil title writes none.
func openLink(w util.BufWriter, dest, title []byte) error {
	if !webURL(dest) {
		return fmt.Errorf("markdown link to %q is outside the allowed subset", dest)
	}
	_, _ = w.WriteString(`<a href="`)
	_, _ = w.Write(util.EscapeHTML(util.URLEscape(dest, true)))
	_, _ = w.WriteString(`" rel="` + linkRel + `"`)
	if title != nil {
		_, _ = w.WriteString(` title="`)
		// Through goldmark's writer, as link and paragraph text are: it
		// resolves backslash escapes and entities, then escapes for HTML.
		html.DefaultWriter.Write(w, title)
		_ = w.WriteByte('"')
	}
	_ = w.WriteByte('>')
	return nil
}
