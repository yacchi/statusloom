package config

// This file implements the DSL-document draft channel (markup.md "draft共有"):
// a single shared <tool>.draft.xml file, sibling to the saved <tool>.xml, that
// the web configurator writes on every edit and that `statusloom monitor
// --draft` and the `statusloom draft` subcommands read and write. It holds the
// raw DSL source text (which may be in-progress and invalid).

import (
	"errors"
	"os"
	"path/filepath"
)

// DraftDocumentPath returns the shared draft-document location for a tool:
// <configDir>/<tool>.draft.xml. The file need not exist. On an unresolvable
// config directory it degrades to a bare "<tool>.draft.xml" relative path,
// mirroring DocumentPath.
func DraftDocumentPath(tool string) string {
	dir, err := configDir()
	if err != nil {
		return tool + ".draft.xml"
	}
	return filepath.Join(dir, tool+".draft.xml")
}

// DraftDocumentExists reports whether the <tool>.draft.xml draft is present.
func DraftDocumentExists(tool string) bool {
	_, err := os.Stat(DraftDocumentPath(tool))
	return err == nil
}

// LoadDraftDocumentSource returns the shared draft's DSL source. exists is
// true only when the draft file itself is present; when it is absent the
// source falls back to the saved <tool>.xml, and finally to
// DefaultDocument(tool), so a caller always has renderable source. A read
// error other than "not found" is returned as err.
//
// Draft source is intentionally NOT parsed/validated here: the draft is a
// last-writer-wins text-sharing channel that tolerates in-progress, invalid
// input (markup.md "draft共有"); callers decide what to do with an invalid
// draft (the web UI shows diagnostics, monitor falls back to the saved
// document).
func LoadDraftDocumentSource(tool string) (source string, exists bool, err error) {
	data, rerr := os.ReadFile(DraftDocumentPath(tool))
	if rerr == nil {
		return string(data), true, nil
	}
	if !errors.Is(rerr, os.ErrNotExist) {
		return "", false, rerr
	}

	// No draft: fall back to the saved document, then the built-in default.
	saved, serr := os.ReadFile(DocumentPath(tool))
	if serr == nil {
		return string(saved), false, nil
	}
	if !errors.Is(serr, os.ErrNotExist) {
		return "", false, serr
	}
	return DefaultDocument(tool), false, nil
}

// SaveDraftDocumentSource atomically writes src to <tool>.draft.xml, reusing
// the document write discipline (sibling temp file + fsync + rename). It does
// not validate src (see LoadDraftDocumentSource).
func SaveDraftDocumentSource(tool, src string) error {
	return writeFileAtomic(DraftDocumentPath(tool), []byte(src))
}
