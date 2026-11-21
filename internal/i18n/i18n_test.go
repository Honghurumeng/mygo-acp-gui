package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	cases := []struct {
		setting   string
		preferred []string
		want      string
	}{
		{"", nil, English},
		{System, nil, English},
		{System, []string{"zh-Hans-CN"}, Chinese},
		{System, []string{"zh-TW", "en"}, Chinese},
		{System, []string{"de-DE", "zh-CN"}, Chinese},
		{System, []string{"de-DE"}, English},
		{"zh", nil, Chinese},
		{"zh-CN", nil, Chinese},
		{"zh-Hans", nil, Chinese},
		{"en", nil, English},
		{"en-GB", nil, English},
		{"fr", nil, English},
		{System, []string{"zh-Hant-TW"}, Chinese},
	}
	for _, c := range cases {
		if got := Resolve(c.setting, c.preferred); got != c.want {
			t.Errorf("Resolve(%q, %v) = %q, want %q", c.setting, c.preferred, got, c.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"":          System,
		"system":    System,
		"en":        English,
		"en-US":     English,
		"zh":        Chinese,
		"zh-CN":     Chinese,
		"zh-Hant":   Chinese, // the app is written in Simplified Chinese only
		"fr":        System,
		" nonsense": System,
	}
	for setting, want := range cases {
		if got := Normalize(setting); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", setting, got, want)
		}
	}
}

func TestTFollowsTheLanguage(t *testing.T) {
	t.Cleanup(func() { Set(English) })
	message := "Settings"
	if T(message) != message {
		t.Fatalf("English T(%q) = %q", message, T(message))
	}
	Set(Chinese)
	if got := T(message); got == message || got == "" {
		t.Fatalf("Chinese T(%q) = %q, want a translation", message, got)
	}
	if got := T("no such message"); got != "no such message" {
		t.Fatalf("missing message = %q, want the English text", got)
	}
	if got := Tf("Shown now: %s.", "简体中文"); got != "当前语言：简体中文。" {
		t.Fatalf("Tf = %q", got)
	}
	if got := S("自动", "auto"); got != "自动" {
		t.Fatalf("S = %q", got)
	}
}

// verb matches the fmt verbs a message may carry, in order.
var verb = regexp.MustCompile(`%\[[0-9]+\][a-zA-Z]|%[+\-#0-9.]*[a-zA-Z%]`)

func verbs(message string) []string {
	var out []string
	for _, m := range verb.FindAllString(message, -1) {
		if m == "%%" {
			continue
		}
		out = append(out, m)
	}
	return out
}

// sameInChinese are the messages Chinese keeps as they are: the ACP term for
// an agent, the names of the viewer's diff, and a percentage.
var sameInChinese = map[string]bool{
	"Agent":     true,
	"Agent %s":  true,
	"Diff":      true,
	"Diff · %s": true,
	"%d%%":      true,
}

// TestCatalog checks that the Chinese catalog only translates messages the
// app says, and that every translation keeps the verbs of its message.
func TestCatalog(t *testing.T) {
	for message, text := range catalog() {
		if strings.TrimSpace(text) == "" {
			t.Errorf("%q has an empty translation", message)
		}
		want, got := verbs(message), verbs(text)
		if strings.Join(want, " ") != strings.Join(got, " ") {
			t.Errorf("%q: verbs %v, translation %q has %v", message, want, text, got)
		}
		if message == text && !sameInChinese[message] {
			t.Errorf("%q translates to itself", message)
		}
	}
}

// TestUIStringsAreTranslated checks the messages the UI wraps in T and Tf:
// every one needs a Chinese entry, so a new screen cannot forget it, and each
// needs the verbs that Tf formats.
func TestUIStringsAreTranslated(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "ui", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	zh := catalog()
	seen := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			// TS writes both texts inline; only T and Tf read the catalog.
			if !ok || (id.Name != "T" && id.Name != "Tf") {
				return true
			}
			first, ok := call.Args[0].(*ast.BasicLit)
			if !ok || first.Kind != token.STRING {
				return true
			}
			message, err := strconv.Unquote(first.Value)
			if err != nil {
				return true
			}
			seen++
			text, ok := zh[message]
			if !ok {
				t.Errorf("%s: %q has no Chinese translation", fset.Position(first.Pos()), message)
				return true
			}
			if id.Name != "Tf" {
				return true
			}
			if strings.Join(verbs(message), " ") != strings.Join(verbs(text), " ") {
				t.Errorf("%s: %q has verbs %v, its translation %v", fset.Position(first.Pos()), message, verbs(message), verbs(text))
			}
			return true
		})
	}
	if seen < 100 {
		t.Errorf("found only %d wrapped messages; the scan is probably broken", seen)
	}
}
