package render

import (
	"os"

	"github.com/yacchi/statusloom/internal/jsonselect"
	"github.com/yacchi/statusloom/internal/schema"
)

// SelectVars maps jsonselect template variables to snapshot values. The
// renderer and EvalSelect share it so a ${...} reference means the same thing
// in both. getenv is consulted only when a variable that needs it is looked
// up, and a nil getenv falls back to os.Getenv.
func SelectVars(snap schema.StatusSnapshot, getenv func(string) string) jsonselect.Vars {
	if getenv == nil {
		getenv = os.Getenv
	}
	return func(name string) string {
		switch name {
		case "session_id":
			return snap.Session.ID
		case "cwd":
			return snap.System.Cwd
		case "workspace.project_dir":
			return snap.System.ProjectDir
		case "CLAUDE_CONFIG_DIR":
			return getenv("CLAUDE_CONFIG_DIR")
		}
		return ""
	}
}

// SelectResult is the outcome of evaluating a source field's select expression
// against a decoded document.
type SelectResult struct {
	Value   any    // the selected JSON value; nil when !Matched (or when a JSON null was selected)
	Matched bool   // the path exists (a JSON null at the path counts as matched)
	Display string // jsonselect.Canonical(Value) when Value is a scalar, else ""
	Err     error  // select syntax error (*jsonselect.SyntaxError); Matched is false then
}

// EvalSelect evaluates sel against doc with the ${...} variables taken from snap and
// getenv (SelectVars; getenv nil means os.Getenv) - the same substitution and
// selection the renderer uses. sel "" selects the whole document.
func EvalSelect(doc any, sel string, snap schema.StatusSnapshot, getenv func(string) string) SelectResult {
	expr, err := jsonselect.Parse(sel)
	if err != nil {
		return SelectResult{Err: err}
	}
	v, ok := expr.Eval(doc, SelectVars(snap, getenv))
	if !ok {
		return SelectResult{}
	}
	display, _ := jsonselect.Canonical(v)
	return SelectResult{Value: v, Matched: true, Display: display}
}
