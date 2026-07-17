package exchange

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Meta represents metadata of a statusloom configuration document.
//
// Notes is free-form Markdown prose the file's author wrote between the
// frontmatter's closing "---" and the first ```xml fence (a heading,
// explanation, usage caveats, whatever). It is deliberately not a
// frontmatter key: frontmatter values are single-line JSON scalars (§6.5),
// which cannot hold multi-paragraph Markdown. Text after the fence is not
// part of Notes (or of anything) — it stays ignored, as before.
type Meta struct {
	Name        string
	Description string
	Author      string
	Notes       string
}

// Decode extracts Meta and DSL XML from a Markdown exchange format (*.sloom.md).
// Input must start with a frontmatter block (---...---) containing format, formatVersion, and optional name/description/author.
// Returns error if frontmatter or ```xml fence is missing or invalid.
func Decode(data []byte) (Meta, string, error) {
	// Remove UTF-8 BOM if present
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}

	// Normalize CRLF to LF
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))

	text := string(data)
	lines := strings.Split(text, "\n")

	if len(lines) == 0 || lines[0] != "---" {
		return Meta{}, "", fmt.Errorf("frontmatter must start with --- at line 1")
	}

	// Find closing ---
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx == -1 {
		return Meta{}, "", fmt.Errorf("frontmatter not closed (no closing ---)")
	}

	// Parse frontmatter
	frontmatter := make(map[string]interface{})
	for i := 1; i < closeIdx; i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue // Skip empty lines
		}

		// Parse "key: value" format
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return Meta{}, "", fmt.Errorf("invalid frontmatter line: %q", line)
		}

		key := strings.TrimSpace(parts[0])
		valueStr := strings.TrimSpace(parts[1])

		// Decode value as JSON scalar
		var value interface{}
		if err := json.Unmarshal([]byte(valueStr), &value); err != nil {
			return Meta{}, "", fmt.Errorf("invalid JSON value for key %q: %q", key, valueStr)
		}

		frontmatter[key] = value
	}

	// Check required keys and validate types
	format, ok := frontmatter["format"]
	if !ok {
		return Meta{}, "", fmt.Errorf("missing required key: format")
	}
	formatStr, ok := format.(string)
	if !ok {
		return Meta{}, "", fmt.Errorf("format must be a string, got %T", format)
	}
	if formatStr != "statusloom/config" {
		return Meta{}, "", fmt.Errorf("format must be \"statusloom/config\", got %q", formatStr)
	}

	formatVersion, ok := frontmatter["formatVersion"]
	if !ok {
		return Meta{}, "", fmt.Errorf("missing required key: formatVersion")
	}
	fv, ok := formatVersion.(float64)
	if !ok {
		return Meta{}, "", fmt.Errorf("formatVersion must be a number, got %T", formatVersion)
	}
	if fv != 1.0 {
		return Meta{}, "", fmt.Errorf("formatVersion must be 1, got %v", fv)
	}

	// Extract optional Meta fields - must be strings
	meta := Meta{}
	if name, ok := frontmatter["name"]; ok {
		s, ok := name.(string)
		if !ok {
			return Meta{}, "", fmt.Errorf("name must be a string, got %T", name)
		}
		meta.Name = s
	}
	if description, ok := frontmatter["description"]; ok {
		s, ok := description.(string)
		if !ok {
			return Meta{}, "", fmt.Errorf("description must be a string, got %T", description)
		}
		meta.Description = s
	}
	if author, ok := frontmatter["author"]; ok {
		s, ok := author.(string)
		if !ok {
			return Meta{}, "", fmt.Errorf("author must be a string, got %T", author)
		}
		meta.Author = s
	}

	// Scan the region after the frontmatter for top-level fenced code blocks,
	// in order, stopping at the first ```xml-opened one. Notes prose may
	// itself contain non-xml fenced examples (e.g. a ```bash snippet); the
	// scan must still walk past those as paired fences (rather than treating
	// any bare ``` line as a boundary) so their content isn't mistaken for
	// the real config or split apart. Per §6.6 the real config is the
	// *first* ```xml fence; everything from its closing ``` line to EOF is
	// ignored as before, including any further ```xml blocks an author may
	// append afterward (e.g. a trailing example/legacy config kept for
	// reference) — those are just ordinary ignored trailing text, not an
	// alternate real fence.
	fenceStart, fenceEnd := -1, -1
	sawUnclosedFence := false
	for i := closeIdx + 1; i < len(lines); {
		if !strings.HasPrefix(lines[i], "```") {
			i++
			continue
		}
		isXML := strings.HasPrefix(lines[i], "```xml")
		closeJ := -1
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(lines[j], "```") {
				closeJ = j
				break
			}
		}
		if closeJ == -1 {
			if isXML {
				sawUnclosedFence = true
			}
			break
		}
		if isXML {
			fenceStart, fenceEnd = i, closeJ
			break
		}
		i = closeJ + 1
	}

	if fenceStart == -1 {
		if sawUnclosedFence {
			return Meta{}, "", fmt.Errorf("unclosed ```xml fence (no closing ```)")
		}
		return Meta{}, "", fmt.Errorf("no ```xml code fence found")
	}

	// Everything between the frontmatter's closing "---" and the first
	// ```xml fence line is free-form prose, preserved verbatim (trimmed of
	// leading/trailing blank lines only, so internal Markdown structure —
	// including any non-xml example fences the author wrote — survives) as
	// Notes. A prose-free document (just a blank line, as Encode writes when
	// there's nothing to say) trims to empty and Notes stays "".
	noteLines := lines[closeIdx+1 : fenceStart]
	if notes := strings.TrimSpace(strings.Join(noteLines, "\n")); notes != "" {
		meta.Notes = notes
	}

	// Extract XML content
	xmlLines := lines[fenceStart+1 : fenceEnd]
	xmlSource := strings.Join(xmlLines, "\n")

	return meta, xmlSource, nil
}

// Encode generates a Markdown exchange format from Meta and DSL XML.
// Text fields are JSON-encoded with quotes to preserve special characters.
// Empty name/description/author fields are omitted.
func Encode(meta Meta, xmlSource string) []byte {
	var buf bytes.Buffer

	// Write frontmatter
	buf.WriteString("---\n")
	buf.WriteString("format: \"statusloom/config\"\n")
	buf.WriteString("formatVersion: 1\n")

	// Write optional Meta fields (only if non-empty)
	if meta.Name != "" {
		nameJSON, _ := json.Marshal(meta.Name)
		fmt.Fprintf(&buf, "name: %s\n", nameJSON)
	}
	if meta.Description != "" {
		descJSON, _ := json.Marshal(meta.Description)
		fmt.Fprintf(&buf, "description: %s\n", descJSON)
	}
	if meta.Author != "" {
		authorJSON, _ := json.Marshal(meta.Author)
		fmt.Fprintf(&buf, "author: %s\n", authorJSON)
	}

	buf.WriteString("---\n\n")

	// Write free-form Notes prose (if any) between the frontmatter and the
	// fence, never as a frontmatter key (see Meta's doc comment).
	if meta.Notes != "" {
		buf.WriteString(meta.Notes)
		if !strings.HasSuffix(meta.Notes, "\n") {
			buf.WriteString("\n")
		}
		buf.WriteString("\n")
	}

	// Write XML fence
	buf.WriteString("```xml\n")
	buf.WriteString(xmlSource)
	// Ensure XML ends with newline
	if !strings.HasSuffix(xmlSource, "\n") {
		buf.WriteString("\n")
	}
	buf.WriteString("```\n")

	return buf.Bytes()
}
