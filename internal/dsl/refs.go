package dsl

import "sort"

// refWalker visits every reference-bearing part of a document: fields, raw
// `optional` attribute values and the metric references inside when /
// color-rule expressions. It walks all layouts (not only the active one) and
// every responsive variant, span, text and <subagent> region, so callers see
// the whole document. It never mutates the document and never validates it.
type refWalker struct {
	onField    func(f *FieldNode)
	onOptional func(raw string)
	onMetric   func(name, param string)
}

// walkRefs drives w over doc. A nil document or a document without a root
// visits nothing.
func walkRefs(doc *Document, w refWalker) {
	if doc == nil || doc.Root == nil {
		return
	}
	for _, layout := range doc.Root.Layouts {
		if layout == nil {
			continue
		}
		for _, child := range layout.Children {
			switch c := child.(type) {
			case *LineNode:
				w.line(c)
			case *ResponsiveNode:
				if c == nil {
					continue
				}
				for _, v := range c.Variants {
					if v == nil {
						continue
					}
					w.expr(v.When)
					for _, l := range v.Lines {
						w.line(l)
					}
					w.subagent(v.Subagent)
				}
			}
		}
		w.subagent(layout.Subagent)
	}
}

func (w refWalker) line(l *LineNode) {
	if l == nil {
		return
	}
	w.common(l.Common)
	w.children(l.Children)
}

func (w refWalker) subagent(s *SubagentNode) {
	if s == nil {
		return
	}
	w.line(s.Line)
}

func (w refWalker) children(nodes []Node) {
	for _, n := range nodes {
		switch n := n.(type) {
		case *SpanNode:
			if n == nil {
				continue
			}
			w.common(n.Common)
			w.children(n.Children)
		case *TextNode:
			if n == nil {
				continue
			}
			w.common(n.Common)
		case *FieldNode:
			if n == nil {
				continue
			}
			if w.onField != nil {
				w.onField(n)
			}
			w.common(n.Common)
		}
	}
}

func (w refWalker) common(c CommonAttributes) {
	if c.Optional != "" && w.onOptional != nil {
		w.onOptional(c.Optional)
	}
	w.expr(c.When)
	for _, rule := range c.ColorRules {
		w.expr(rule.When)
	}
}

// expr reports the metric references of one condition expression. A
// expression that does not parse is skipped silently: refs are read from the
// AST, and validation is what reports the syntax error.
func (w refWalker) expr(src string) {
	if src == "" || w.onMetric == nil {
		return
	}
	e, err := ParseCondition(src)
	if err != nil || e == nil {
		return
	}
	for _, m := range e.Metrics() {
		if m == "self" {
			continue
		}
		name, param := ParseMetricRef(m)
		w.onMetric(name, param)
	}
}

// ReferencedSources returns every external source id the document references:
// the source attribute of name="source" fields (non-empty only),
// optional="source:<id>", and source-stale:<id> metrics in when / color-rule
// expressions - across all layouts, responsive variants (including variant
// when), lines, spans, fields, texts and <subagent> regions. Sorted and
// deduplicated. Unparsable expressions are skipped. A nil document or nil
// Root yields nil.
func ReferencedSources(doc *Document) []string {
	set := map[string]struct{}{}
	walkRefs(doc, refWalker{
		onField: func(f *FieldNode) {
			if f.Name == "source" && f.Source != "" {
				set[f.Source] = struct{}{}
			}
		},
		onOptional: func(raw string) {
			if field, id := ParseOptional(raw); field == "source" && id != "" {
				set[id] = struct{}{}
			}
		},
		onMetric: func(name, param string) {
			if name == "source-stale" && param != "" {
				set[param] = struct{}{}
			}
		},
	})
	if len(set) == 0 {
		return nil
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// UsesTranscript reports whether the document references any transcript-derived
// value: a <field> whose FieldDef has Transcript, a name="source" field whose
// Fallback names such a field (the fallback renders that value), an optional=
// naming such a field, or a when / color-rule expression referencing a metric
// whose MetricDef has Transcript. Fields and metrics are looked up in the
// catalog of doc.Root.Tool. A nil document or nil Root yields false.
func UsesTranscript(doc *Document) bool {
	if doc == nil || doc.Root == nil {
		return false
	}
	tool := doc.Root.Tool
	found := false
	walkRefs(doc, refWalker{
		onField: func(f *FieldNode) {
			if fieldIsTranscript(tool, f.Name) {
				found = true
			}
			if f.Name == "source" && f.Fallback != "" && fieldIsTranscript(tool, f.Fallback) {
				found = true
			}
		},
		onOptional: func(raw string) {
			if field, _ := ParseOptional(raw); fieldIsTranscript(tool, field) {
				found = true
			}
		},
		onMetric: func(name, _ string) {
			if metricIsTranscript(tool, name) {
				found = true
			}
		},
	})
	return found
}

func fieldIsTranscript(tool, name string) bool {
	f, ok := FieldByName(tool, name)
	return ok && f.Transcript
}

func metricIsTranscript(tool, name string) bool {
	m, ok := MetricByName(tool, name)
	return ok && m.Transcript
}
