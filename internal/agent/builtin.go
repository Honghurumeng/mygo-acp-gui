package agent

import "fmt"

// BuiltinAgent is the hand-maintained agent table. Adding an agent is a code
// change plus a release, never runtime discovery.
type BuiltinAgent struct {
	ID          string
	Name        string
	InstallHint string
}

// Builtins is the fixed list this app supports.
var Builtins = []BuiltinAgent{
	{
		ID:          PiID,
		Name:        "pi",
		InstallHint: "Install Node.js 22+, pi, and the vendored pi-acp bundle (see docs/design/pi.md).",
	},
	{
		ID:          CommandCodeID,
		Name:        "Command Code",
		InstallHint: "Install command-code with Node.js 22+, then run: command-code login",
	},
	{
		ID:          CodexID,
		Name:        "Codex",
		InstallHint: "Install Node.js 22+ and the Codex CLI, then run: codex login (see docs/design/codex.md).",
	},
}

// IsBuiltinID reports whether id belongs to the hand-maintained table.
func IsBuiltinID(id string) bool {
	for _, builtin := range Builtins {
		if builtin.ID == id {
			return true
		}
	}
	return false
}

// LaunchResult is one builtin agent's resolved launch spec or the reason it
// could not be built.
type LaunchResult struct {
	BuiltinAgent
	Spec LaunchSpec
	Err  error
}

// BuiltinLaunchResults resolves every builtin agent independently. A
// missing or invalid agent does not hide the others.
func BuiltinLaunchResults(resourcesDir string, env Environment) []LaunchResult {
	results := make([]LaunchResult, 0, len(Builtins))
	for _, builtin := range Builtins {
		var spec LaunchSpec
		var err error
		switch builtin.ID {
		case PiID:
			spec, err = PiLaunchSpec(resourcesDir, env)
		case CommandCodeID:
			spec, err = CommandCodeLaunchSpec(env)
		case CodexID:
			spec, err = CodexLaunchSpec(resourcesDir, env)
		default:
			err = fmt.Errorf("unknown builtin agent: %s", builtin.ID)
		}
		results = append(results, LaunchResult{BuiltinAgent: builtin, Spec: spec, Err: err})
	}
	return results
}
