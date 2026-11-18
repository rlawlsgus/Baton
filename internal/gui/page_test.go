package gui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"baton/internal/convdel"
	"baton/internal/desklogin"
)

// The page's texts and how it fills values into them, read from app.js as
// the window serves it. No browser runs here; page_live_test.go has one.

func appJS(t *testing.T) string {
	t.Helper()
	b, err := fs.ReadFile(webFS, "web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// pageTexts reads the keys of the page's texts (T in app.js), by language.
func pageTexts(t *testing.T, js string) map[string][]string {
	t.Helper()
	const head = "const T = "
	i := strings.Index(js, head)
	if i < 0 {
		t.Fatal("app.js has no texts")
	}
	out := map[string][]string{}
	lang, depth := "", 0
	for i += len(head); i < len(js); i++ {
		c := js[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			i = skipString(js, i)
		case strings.HasPrefix(js[i:], "//"):
			if n := strings.IndexByte(js[i:], '\n'); n > 0 {
				i += n
			}
		case c == '{' || c == '[' || c == '(':
			depth++
		case c == '}' || c == ']' || c == ')':
			if depth--; depth == 0 {
				return out
			}
		case isIdent(c) && !isIdent(js[i-1]):
			j := i
			for j < len(js) && isIdent(js[j]) {
				j++
			}
			if strings.HasPrefix(strings.TrimLeft(js[j:], " "), ":") {
				switch depth {
				case 1:
					lang = js[i:j]
				case 2:
					out[lang] = append(out[lang], js[i:j])
				}
			}
			i = j - 1
		}
	}
	t.Fatal("the texts in app.js do not end")
	return nil
}

func isIdent(c byte) bool {
	return c == '_' || c == '$' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// skipString returns where the string literal that starts at js[i] ends.
func skipString(js string, i int) int {
	q := js[i]
	for i++; i < len(js) && js[i] != q; i++ {
		if js[i] == '\\' {
			i++
		}
	}
	return i
}

// Every text is there in English and in Korean.
func TestPageTextsInBothLanguages(t *testing.T) {
	texts := pageTexts(t, appJS(t))
	en, ko := texts["en"], texts["ko"]
	if len(en) < 500 || len(texts) != 2 {
		t.Fatalf("read %d languages, %d English texts", len(texts), len(en))
	}
	for _, k := range en {
		if !slices.Contains(ko, k) {
			t.Errorf("%s: no Korean text", k)
		}
	}
	for _, k := range ko {
		if !slices.Contains(en, k) {
			t.Errorf("%s: no English text", k)
		}
	}
}

// The codes a delete or the desktop app's sign-ins can be refused with are
// worded in the window's language, never shown as baton's English message.
func TestPageWordsEveryRefusal(t *testing.T) {
	texts := pageTexts(t, appJS(t))
	codes := []string{
		// a delete (internal/convdel), and this window's own
		convdel.CodeBadSession, convdel.CodeGone, convdel.CodeDeleting, convdel.CodeInBaton, convdel.CodeCopyStarting,
		convdel.CodeOpenElsewhere, convdel.CodeCannotCheck, convdel.CodeTwoFiles, convdel.CodeOtherName, convdel.CodeDesktopApp,
		convdel.CodeDesktopBusy, convdel.CodeDesktopWaiting, convdel.CodeNoRecycleBin, convdel.CodePathTooLong, convdel.CodeInUse,
		convdel.CodePartly, convdel.CodeSessionWindow, convdel.CodeLinkedFolder, convdel.CodeRecycleFailed, convdel.CodeChanged,
		"delete",
		// the desktop app's sign-ins (internal/desklogin), and this window's own
		desklogin.CodeOff, desklogin.CodeAppRunning, desklogin.CodeBusy, desklogin.CodeNotKept, desklogin.CodeAmbiguous,
		desklogin.CodeDamaged, desklogin.CodeAlready, desklogin.CodeUnknownFormat, desklogin.CodeUnsupportedLayout,
		desklogin.CodeUnsavedCurrent, desklogin.CodeSwitchFailed, desklogin.CodeSwitchUnrestored, desklogin.CodeSyncedStore,
		desklogin.CodeInsideClaude, desklogin.CodeElevated, desklogin.CodeBadLabel, desklogin.CodeLaunchUnsupported,
		desklogin.CodeUsedSince,
		"desktop_logins", "needs_desktop_ack",
		// uninstalling while a switch of the desktop app's sign-in is cut off
		"cut_off_running", "cut_off_inside_claude",
	}
	for _, c := range codes {
		for _, lang := range []string{"en", "ko"} {
			if !slices.Contains(texts[lang], "err_"+c) {
				t.Errorf("%s: no text for %s", lang, c)
			}
		}
	}
}

// replaceArgs are the arguments of every .replace( and .replaceAll( call in
// js, split at their top-level commas.
func replaceArgs(js string) [][]string {
	var out [][]string
	for _, m := range regexp.MustCompile(`\.replace(All)?\(`).FindAllStringIndex(js, -1) {
		var args []string
		depth, start := 0, m[1]
	scan:
		for i := m[1] - 1; i < len(js); i++ {
			switch c := js[i]; {
			case c == '\'' || c == '"' || c == '`':
				i = skipString(js, i)
			case c == '/' && depth == 1 && strings.TrimSpace(js[start:i]) == "":
				// A regular expression, which starts an argument here.
				for i++; i < len(js) && js[i] != '/'; i++ {
					switch js[i] {
					case '\\':
						i++
					case '[':
						for i++; i < len(js) && js[i] != ']'; i++ {
							if js[i] == '\\' {
								i++
							}
						}
					}
				}
			case c == '(' || c == '[' || c == '{':
				depth++
			case c == ')' || c == ']' || c == '}':
				if depth--; depth == 0 {
					args = append(args, strings.TrimSpace(js[start:i]))
					break scan
				}
			case c == ',' && depth == 1:
				args = append(args, strings.TrimSpace(js[start:i]))
				start = i + 1
			}
		}
		out = append(out, args)
	}
	return out
}

// A name, an email or a path goes into a text as it is: a string given to
// replace reads $&, $' and $$ in it as patterns, so a value goes in by a
// function, and only fixed text as a string.
func TestPageInsertsValuesAsTheyAre(t *testing.T) {
	fixed := regexp.MustCompile(`^('([^'\\]|\\.)*'|"([^"\\]|\\.)*"|` + "`[^`$]*`" + `)$`)
	byFunc := regexp.MustCompile(`^(\([^()]*\)|[\w$]+)\s*=>|^function\b`)
	calls := replaceArgs(appJS(t))
	if len(calls) < 5 {
		t.Fatalf("found %d calls", len(calls))
	}
	for _, args := range calls {
		if len(args) != 2 {
			t.Errorf("replace(%s): not two arguments", strings.Join(args, ", "))
			continue
		}
		if r := args[1]; !fixed.MatchString(r) && !byFunc.MatchString(r) {
			t.Errorf("replace(%s, %s): a value given as a string", args[0], r)
		}
	}
}

// codesIn reads the codes a package names (its Code* string constants) from
// its source file.
func codesIn(t *testing.T, file string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, sp := range g.Specs {
			v := sp.(*ast.ValueSpec)
			for i, n := range v.Names {
				if i >= len(v.Values) || !strings.HasPrefix(n.Name, "Code") {
					continue
				}
				if lit, ok := v.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					out = append(out, strings.Trim(lit.Value, `"`))
				}
			}
		}
	}
	if len(out) < 10 {
		t.Fatalf("%s: read only %d codes", file, len(out))
	}
	return out
}

// A code added to convdel or desklogin later is worded too: every one their
// sources name has a text in both languages.
func TestPageWordsEveryCodeOfTheirSources(t *testing.T) {
	texts := pageTexts(t, appJS(t))
	for _, file := range []string{filepath.Join("..", "convdel", "convdel.go"), filepath.Join("..", "desklogin", "desklogin.go")} {
		for _, c := range codesIn(t, file) {
			for _, lang := range []string{"en", "ko"} {
				if !slices.Contains(texts[lang], "err_"+c) {
					t.Errorf("%s: no text for %s (%s)", lang, c, file)
				}
			}
		}
	}
}
