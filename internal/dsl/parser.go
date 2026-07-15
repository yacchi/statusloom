package dsl

import (
	"encoding/xml"
	"io"
	"strings"
)

// Parse parses DSL source into a Document AST, returning any diagnostics
// found. XML well-formedness violations are fatal: they are reported as
// an error diagnostic and Parse returns doc=nil. Recoverable structural
// problems (unknown elements/attributes, type-invalid attribute values,
// invalid nesting, disallowed newlines in <text>, and so on) are reported
// as diagnostics while Parse still builds as much of the AST as it can.
//
// Semantic checks that depend on the registry/condition/formatter layers
// (field existence, formatter applicability, when-expression metrics,
// color formats, layout activeness, enum settings) are performed by
// Validate, not here.
func Parse(src string) (*Document, []Diagnostic) {
	p := &parser{src: src, dec: xml.NewDecoder(strings.NewReader(src))}
	p.dec.Strict = true
	root := p.parseDocument()
	if p.fatal != nil {
		return nil, p.diags
	}
	return &Document{Source: src, Root: root}, p.diags
}

type parser struct {
	src        string
	dec        *xml.Decoder
	diags      []Diagnostic
	prevOffset int   // InputOffset() after the last token = start of the next
	fatal      error // set on an XML well-formedness violation
}

func (p *parser) errf(r SourceRange, format string, args ...any) {
	p.diags = append(p.diags, Errorf(r, format, args...))
}

// token reads the next XML token. It returns the byte offset at which the
// token began (i.e. the '<' of an element) and ok=false on EOF or on a
// fatal syntax error (which it records). CharData/Comment byte slices are
// copied because the decoder reuses their backing storage.
func (p *parser) token() (xml.Token, int, bool) {
	if p.fatal != nil {
		return nil, p.prevOffset, false
	}
	start := p.prevOffset
	tok, err := p.dec.Token()
	p.prevOffset = int(p.dec.InputOffset())
	if err != nil {
		if err == io.EOF {
			return nil, start, false
		}
		p.fatal = err
		p.errf(SourceRange{Start: start, End: p.prevOffset}, "XML syntax error: %v", err)
		return nil, start, false
	}
	switch t := tok.(type) {
	case xml.CharData:
		b := make([]byte, len(t))
		copy(b, t)
		return xml.CharData(b), start, true
	case xml.Comment:
		b := make([]byte, len(t))
		copy(b, t)
		return xml.Comment(b), start, true
	}
	return tok, start, true
}

// skipElement consumes tokens until the currently-open element (whose
// StartElement was just read) is balanced by its EndElement, keeping the
// decoder in sync when an element is dropped.
func (p *parser) skipElement() {
	depth := 1
	for depth > 0 {
		tok, _, ok := p.token()
		if !ok {
			return
		}
		switch tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
}

func (p *parser) strayText(t xml.CharData, start int) {
	if strings.TrimSpace(string(t)) != "" {
		p.errf(SourceRange{Start: start, End: p.prevOffset}, "unexpected text content %q", string(t))
	}
}

// parseDocument reads top-level tokens, parsing the single root
// <statusloom> element.
func (p *parser) parseDocument() *StatusloomNode {
	var root *StatusloomNode
	for {
		tok, start, ok := p.token()
		if !ok {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if root != nil {
				p.skipElement()
				p.errf(SourceRange{Start: start, End: p.prevOffset}, "unexpected element <%s> after root element", t.Name.Local)
				continue
			}
			if t.Name.Local != "statusloom" {
				p.errf(SourceRange{Start: start, End: p.prevOffset}, "root element must be <statusloom>, got <%s>", t.Name.Local)
			}
			root = p.parseStatusloom(t, start)
		case xml.CharData:
			p.strayText(t, start)
		case xml.Comment, xml.ProcInst, xml.Directive:
			// XML declaration, DTD, and pre-root comments are ignored.
		}
	}
	if root == nil && p.fatal == nil {
		p.errf(SourceRange{}, "missing root <statusloom> element")
	}
	return root
}

func (p *parser) parseStatusloom(se xml.StartElement, start int) *StatusloomNode {
	openEnd := p.prevOffset
	tagRange := SourceRange{Start: start, End: openEnd}
	n := &StatusloomNode{}
	for _, a := range se.Attr {
		switch a.Name.Local {
		case "version":
			n.Version = a.Value
		case "tool":
			n.Tool = a.Value
		case "color-level":
			n.Settings.ColorLevel = a.Value
		case "output-style":
			n.Settings.OutputStyle = a.Value
		case "context-percentage-mode":
			n.Settings.ContextPercentageMode = a.Value
		case "compact-threshold":
			n.Settings.CompactThreshold = p.parseIntAttr("compact-threshold", a.Value, tagRange)
		case "context-reserve-tokens":
			n.Settings.ContextReserveTokens = p.parseIntAttr("context-reserve-tokens", a.Value, tagRange)
		default:
			p.errf(tagRange, "unknown attribute %q on <statusloom>", a.Name.Local)
		}
	}
	for {
		tok, tstart, ok := p.token()
		if !ok {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "git":
				g := p.parseGit(t, tstart)
				if n.Git != nil {
					p.errf(g.Meta.SourceRange, "at most one <git> element is allowed")
				} else {
					n.Git = g
				}
			case "layout":
				n.Layouts = append(n.Layouts, p.parseLayout(t, tstart))
			default:
				p.skipElement()
				p.errf(SourceRange{Start: tstart, End: p.prevOffset}, "unknown element <%s> inside <statusloom>; expected <git> or <layout>", t.Name.Local)
			}
		case xml.Comment:
			n.Comments = append(n.Comments, &CommentNode{
				Meta: NodeMeta{SourceRange: SourceRange{Start: tstart, End: p.prevOffset}},
				Text: string(t),
			})
		case xml.CharData:
			p.strayText(t, tstart)
		case xml.EndElement:
			n.Meta.SourceRange = SourceRange{Start: start, End: p.prevOffset}
			return n
		}
	}
	n.Meta.SourceRange = SourceRange{Start: start, End: p.prevOffset}
	return n
}

func (p *parser) parseGit(se xml.StartElement, start int) *GitSettings {
	openEnd := p.prevOffset
	tagRange := SourceRange{Start: start, End: openEnd}
	g := &GitSettings{}
	for _, a := range se.Attr {
		switch a.Name.Local {
		case "cache-ttl-ms":
			g.CacheTTLMS = p.parseIntAttr("cache-ttl-ms", a.Value, tagRange)
		case "timeout-ms":
			g.TimeoutMS = p.parseIntAttr("timeout-ms", a.Value, tagRange)
		case "include-untracked":
			g.IncludeUntracked = p.parseBoolAttr("include-untracked", a.Value, tagRange)
		case "collect-numstat":
			g.CollectNumstat = p.parseBoolAttr("collect-numstat", a.Value, tagRange)
		default:
			p.errf(tagRange, "unknown attribute %q on <git>", a.Name.Local)
		}
	}
	p.consumeLeaf(&g.Meta, start, "git")
	return g
}

func (p *parser) parseLayout(se xml.StartElement, start int) *LayoutNode {
	openEnd := p.prevOffset
	tagRange := SourceRange{Start: start, End: openEnd}
	n := &LayoutNode{}
	for _, a := range se.Attr {
		switch a.Name.Local {
		case "name":
			n.Name = a.Value
		case "active":
			n.Active = p.parseBoolAttr("active", a.Value, tagRange)
		default:
			p.errf(tagRange, "unknown attribute %q on <layout>", a.Name.Local)
		}
	}
	for {
		tok, tstart, ok := p.token()
		if !ok {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "line" {
				n.Lines = append(n.Lines, p.parseLine(t, tstart))
			} else {
				p.skipElement()
				p.errf(SourceRange{Start: tstart, End: p.prevOffset}, "unknown element <%s> inside <layout>; expected <line>", t.Name.Local)
			}
		case xml.Comment:
			n.Comments = append(n.Comments, &CommentNode{
				Meta: NodeMeta{SourceRange: SourceRange{Start: tstart, End: p.prevOffset}},
				Text: string(t),
			})
		case xml.CharData:
			p.strayText(t, tstart)
		case xml.EndElement:
			n.Meta.SourceRange = SourceRange{Start: start, End: p.prevOffset}
			return n
		}
	}
	n.Meta.SourceRange = SourceRange{Start: start, End: p.prevOffset}
	return n
}

func (p *parser) parseLine(se xml.StartElement, start int) *LineNode {
	openEnd := p.prevOffset
	tagRange := SourceRange{Start: start, End: openEnd}
	n := &LineNode{}
	var cb commonBuilder
	for _, a := range se.Attr {
		if !cb.consume(p, a, tagRange) {
			p.errf(tagRange, "unknown attribute %q on <line>", a.Name.Local)
		}
	}
	n.Common = cb.build()
	// <line> allows flex but not color-rule.
	n.Meta.SourceRange = p.parseChildren(start, true, false, &n.Children, &n.Common.ColorRules)
	return n
}

func (p *parser) parseSpan(se xml.StartElement, start int) *SpanNode {
	openEnd := p.prevOffset
	tagRange := SourceRange{Start: start, End: openEnd}
	n := &SpanNode{}
	var cb commonBuilder
	for _, a := range se.Attr {
		if !cb.consume(p, a, tagRange) {
			p.errf(tagRange, "unknown attribute %q on <span>", a.Name.Local)
		}
	}
	n.Common = cb.build()
	// <span> allows color-rule but not flex.
	n.Meta.SourceRange = p.parseChildren(start, false, true, &n.Children, &n.Common.ColorRules)
	return n
}

// renderRawText applies the raw-TextNode whitespace rules (markup.md
// "生TextNodeの空白ルール") to produce the display text: leading/trailing
// whitespace is trimmed, and each *internal* whitespace run that contains
// a newline (indentation from wrapping the text across source lines)
// collapses to a single space. Internal runs of spaces/tabs without a
// newline are preserved verbatim. Returns "" for whitespace-only input.
func renderRawText(raw string) string {
	trimmed := strings.Trim(raw, " \t\r\n")
	if trimmed == "" || !strings.ContainsAny(trimmed, "\n\r") {
		return trimmed
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	i := 0
	for i < len(trimmed) {
		c := trimmed[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			j := i
			hasNewline := false
			for j < len(trimmed) {
				d := trimmed[j]
				if d == '\n' || d == '\r' {
					hasNewline = true
				} else if d != ' ' && d != '\t' {
					break
				}
				j++
			}
			if hasNewline {
				b.WriteByte(' ')
			} else {
				b.WriteString(trimmed[i:j])
			}
			i = j
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// parseChildren reads the mixed content of a <line> or <span> until the
// matching EndElement, appending element/raw-text/comment children to out
// and color-rule children to rules. allowFlex/allowColorRule gate the two
// context-specific child kinds; a disallowed one is still consumed (to
// keep the decoder balanced) but reported as an invalid-nesting error.
func (p *parser) parseChildren(start int, allowFlex, allowColorRule bool, out *[]Node, rules *[]ColorRule) SourceRange {
	var rawBuf []byte
	rawStart := 0
	rawActive := false
	flush := func(end int) {
		if !rawActive {
			return
		}
		raw := string(rawBuf)
		rendered := renderRawText(raw)
		if rendered != "" {
			*out = append(*out, &RawTextNode{
				Meta:       NodeMeta{SourceRange: SourceRange{Start: rawStart, End: end}},
				RawValue:   raw,
				RenderText: rendered,
			})
		}
		rawActive = false
		rawBuf = nil
	}
	for {
		tok, tstart, ok := p.token()
		if !ok {
			flush(tstart)
			return SourceRange{Start: start, End: p.prevOffset}
		}
		switch t := tok.(type) {
		case xml.CharData:
			if !rawActive {
				rawActive = true
				rawStart = tstart
				rawBuf = nil
			}
			rawBuf = append(rawBuf, t...)
		case xml.StartElement:
			flush(tstart)
			switch t.Name.Local {
			case "span":
				*out = append(*out, p.parseSpan(t, tstart))
			case "text":
				*out = append(*out, p.parseText(t, tstart))
			case "field":
				*out = append(*out, p.parseField(t, tstart))
			case "flex":
				fn := p.parseFlex(t, tstart)
				if allowFlex {
					*out = append(*out, fn)
				} else {
					p.errf(fn.Meta.SourceRange, "<flex> is only allowed directly inside <line>")
				}
			case "color-rule":
				cr := p.parseColorRule(t, tstart)
				if allowColorRule {
					*rules = append(*rules, cr)
				} else {
					p.errf(cr.Meta.SourceRange, "<color-rule> is not allowed here; it may only appear inside <field>, <span>, or <text>")
				}
			default:
				p.skipElement()
				p.errf(SourceRange{Start: tstart, End: p.prevOffset}, "unknown element <%s>", t.Name.Local)
			}
		case xml.Comment:
			flush(tstart)
			*out = append(*out, &CommentNode{
				Meta: NodeMeta{SourceRange: SourceRange{Start: tstart, End: p.prevOffset}},
				Text: string(t),
			})
		case xml.EndElement:
			flush(tstart)
			return SourceRange{Start: start, End: p.prevOffset}
		}
	}
}

func (p *parser) parseText(se xml.StartElement, start int) *TextNode {
	openEnd := p.prevOffset
	tagRange := SourceRange{Start: start, End: openEnd}
	n := &TextNode{}
	var cb commonBuilder
	for _, a := range se.Attr {
		switch a.Name.Local {
		case "role":
			if a.Value != "" && a.Value != "separator" {
				p.errf(tagRange, "invalid role %q on <text>; only \"separator\" is allowed", a.Value)
			} else {
				n.Role = a.Value
			}
		default:
			if !cb.consume(p, a, tagRange) {
				p.errf(tagRange, "unknown attribute %q on <text>", a.Name.Local)
			}
		}
	}
	n.Common = cb.build()
	var buf []byte
	for {
		tok, tstart, ok := p.token()
		if !ok {
			break
		}
		switch t := tok.(type) {
		case xml.CharData:
			buf = append(buf, t...)
		case xml.StartElement:
			if t.Name.Local == "color-rule" {
				n.Common.ColorRules = append(n.Common.ColorRules, p.parseColorRule(t, tstart))
			} else {
				p.skipElement()
				p.errf(SourceRange{Start: tstart, End: p.prevOffset}, "unknown element <%s> inside <text>", t.Name.Local)
			}
		case xml.Comment:
			// <text> has no children slot for comments; ignore.
		case xml.EndElement:
			n.Value = string(buf)
			if strings.ContainsAny(n.Value, "\n\r") {
				p.errf(SourceRange{Start: start, End: p.prevOffset}, "<text> content may not contain a newline")
			}
			n.Meta.SourceRange = SourceRange{Start: start, End: p.prevOffset}
			return n
		}
	}
	n.Value = string(buf)
	n.Meta.SourceRange = SourceRange{Start: start, End: p.prevOffset}
	return n
}

func (p *parser) parseField(se xml.StartElement, start int) *FieldNode {
	openEnd := p.prevOffset
	tagRange := SourceRange{Start: start, End: openEnd}
	n := &FieldNode{}
	var cb commonBuilder
	for _, a := range se.Attr {
		switch a.Name.Local {
		case "name":
			n.Name = a.Value
		case "format":
			n.Formatter.Name = a.Value
		case "precision":
			n.Formatter.Precision = a.Value
		case "currency":
			n.Formatter.Currency = a.Value
		case "raw":
			if b := p.parseBoolAttr("raw", a.Value, tagRange); b != nil {
				n.Raw = *b
			}
		case "hyperlink":
			if b := p.parseBoolAttr("hyperlink", a.Value, tagRange); b != nil {
				n.Hyperlink = *b
			}
		case "min-width":
			n.MinWidth = p.parsePositiveIntAttr("min-width", a.Value, tagRange)
		case "align":
			n.Align = a.Value
		default:
			if !cb.consume(p, a, tagRange) {
				p.errf(tagRange, "unknown attribute %q on <field>", a.Name.Local)
			}
		}
	}
	n.Common = cb.build()
	for {
		tok, tstart, ok := p.token()
		if !ok {
			break
		}
		switch t := tok.(type) {
		case xml.CharData:
			p.checkFieldStray(t, tstart)
		case xml.StartElement:
			if t.Name.Local == "color-rule" {
				n.Common.ColorRules = append(n.Common.ColorRules, p.parseColorRule(t, tstart))
			} else {
				p.skipElement()
				p.errf(SourceRange{Start: tstart, End: p.prevOffset}, "unknown element <%s> inside <field>; only <color-rule> is allowed", t.Name.Local)
			}
		case xml.Comment:
			// no children slot for comments on <field>; ignore.
		case xml.EndElement:
			n.Meta.SourceRange = SourceRange{Start: start, End: p.prevOffset}
			return n
		}
	}
	n.Meta.SourceRange = SourceRange{Start: start, End: p.prevOffset}
	return n
}

func (p *parser) checkFieldStray(t xml.CharData, start int) {
	if strings.TrimSpace(string(t)) != "" {
		p.errf(SourceRange{Start: start, End: p.prevOffset}, "<field> may not contain text content")
	}
}

func (p *parser) parseFlex(se xml.StartElement, start int) *FlexNode {
	openEnd := p.prevOffset
	tagRange := SourceRange{Start: start, End: openEnd}
	n := &FlexNode{}
	for _, a := range se.Attr {
		if a.Name.Local == "size" {
			n.Size = a.Value
		} else {
			p.errf(tagRange, "unknown attribute %q on <flex>", a.Name.Local)
		}
	}
	p.consumeLeaf(&n.Meta, start, "flex")
	return n
}

func (p *parser) parseColorRule(se xml.StartElement, start int) ColorRule {
	openEnd := p.prevOffset
	tagRange := SourceRange{Start: start, End: openEnd}
	cr := ColorRule{}
	for _, a := range se.Attr {
		switch a.Name.Local {
		case "when":
			cr.When = a.Value
		case "color":
			cr.Color = a.Value
		default:
			p.errf(tagRange, "unknown attribute %q on <color-rule>", a.Name.Local)
		}
	}
	p.consumeLeaf(&cr.Meta, start, "color-rule")
	return cr
}

// consumeLeaf drains the children of an element expected to be empty
// (git/flex/color-rule), reporting a diagnostic for any child element or
// non-whitespace text, and records the element's full source range.
func (p *parser) consumeLeaf(meta *NodeMeta, start int, name string) {
	for {
		tok, tstart, ok := p.token()
		if !ok {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			p.skipElement()
			p.errf(SourceRange{Start: tstart, End: p.prevOffset}, "<%s> may not contain child elements", name)
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				p.errf(SourceRange{Start: tstart, End: p.prevOffset}, "<%s> may not contain text content", name)
			}
		case xml.EndElement:
			meta.SourceRange = SourceRange{Start: start, End: p.prevOffset}
			return
		}
	}
	meta.SourceRange = SourceRange{Start: start, End: p.prevOffset}
}
