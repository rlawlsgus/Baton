package desklogin

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const self = "baton/internal/desklogin"

// moduleRoot is the repository, two folders up from this package.
func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	must(t, err)
	return root
}

// Never automatic: what runs on its own (a session's supervisor, a run, the
// accounts' health) can never reach this package, so nothing switches the
// desktop app's sign-in but the user's click or command.
func TestNoAutomaticSwitch(t *testing.T) {
	root := moduleRoot(t)
	for _, start := range []string{"baton/internal/supervisor", "baton/internal/run", "baton/internal/ledger"} {
		seen := map[string]bool{}
		var walk func(pkg string, path []string)
		walk = func(pkg string, path []string) {
			if seen[pkg] {
				return
			}
			seen[pkg] = true
			if pkg == self {
				t.Errorf("%s reaches %s: %s", start, self, strings.Join(path, " -> "))
				return
			}
			p, err := build.ImportDir(filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(pkg, "baton/"))), 0)
			if err != nil {
				t.Fatalf("%s: %v", pkg, err)
			}
			for _, imp := range p.Imports {
				if strings.HasPrefix(imp, "baton/") {
					walk(imp, append(slices.Clone(path), imp))
				}
			}
		}
		walk(start, []string{start})
		if len(seen) < 2 && start != "baton/internal/ledger" {
			t.Errorf("%s: the walk found nothing (%v)", start, seen)
		}
	}
}

// callers are the only files that may switch, put back or settle a switch
// that was cut off, or open the app: the window's handlers and `baton
// desktop login` (and uninstall, which puts back or settles a cut-off switch
// before it deletes baton's folder).
var callers = map[string][]string{
	"internal/gui/desklogins.go": {"Use", "UseAfterQuit", "Blank", "BlankAfterQuit", "Recover", "Settle", "Launch"},
	"cmd/baton/desktop.go":       {"Use", "UseAfterQuit", "Blank", "BlankAfterQuit", "Recover", "Settle", "Launch"},
	"cmd/baton/install.go":       {"Recover", "Settle"},
}

// switchers are the methods that change which sign-in the app has, or quit
// or open it.
var switchers = []string{"Use", "UseAfterQuit", "Blank", "BlankAfterQuit", "Recover", "Settle", "Launch", "QuitApp"}

// The helpers that hand a *Logins to the window's code, the window's
// handlers and what they switch or open the app through, and the CLI's
// commands and what they open the app through, stay in those files as well
// (the window's route map names its handlers, and the CLI's dispatch its
// commands): a call through them names no method of this package. A method
// is reached only as a selector (s.switched), so only a selector of its
// name counts: a field or a variable named so elsewhere calls nothing (as
// install.go's logins). A function is reached by its name alone
// (settleAndOpen), so every identifier of its name counts.
type helper struct {
	method bool
	files  []string
}

const (
	guiFile    = "internal/gui/desklogins.go"
	guiRoutes  = "internal/gui/api.go"
	cliFile    = "cmd/baton/desktop.go"
	cliMain    = "cmd/baton/main.go"
	cliInstall = "cmd/baton/install.go"
)

var helpers = map[string]helper{
	"logins":           {true, []string{guiFile}},
	"switched":         {true, []string{guiFile}},
	"launchAfter":      {true, []string{guiFile}},
	"apiLoginsUse":     {true, []string{guiFile, guiRoutes}},
	"apiLoginsAdd":     {true, []string{guiFile, guiRoutes}},
	"apiLoginsRecover": {true, []string{guiFile, guiRoutes}},
	"apiLoginsSettle":  {true, []string{guiFile, guiRoutes}},
	"cmdDesktopLogin":  {false, []string{cliFile}},
	"settleAndOpen":    {false, []string{cliFile}},
	"cmdDesktop":       {false, []string{cliFile, cliMain}},
	"cmdUninstall":     {false, []string{cliInstall, cliMain}},
}

// Use, Blank, Recover, Settle and Launch are reached only from those files.
// Every Go file of baton's own is read, not only those that import this
// package: a call through a helper that returns a *Logins, or a method value
// (use := l.Use), names the method just the same, as a selector. A selector
// of that name is allowed elsewhere only as what an assignment sets
// (notify.Notification's Launch field), and in internal/notify, which reads
// that field.
func TestOnlyHandlersSwitch(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()
	files := 0
	declared := map[string]bool{}
	must(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == "node_modules" || n == "bin" || n == "test" || n == "dist" || n == "docs" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "internal/desklogin/") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		files++
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok {
				if h, ok := helpers[fd.Name.Name]; ok && h.method == (fd.Recv != nil) && rel == h.files[0] {
					declared[fd.Name.Name] = true
				}
			}
		}
		set := map[ast.Expr]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			if a, ok := n.(*ast.AssignStmt); ok {
				for _, x := range a.Lhs {
					set[x] = true
				}
			}
			return true
		})
		notify := strings.HasPrefix(rel, "internal/notify/")
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				m := x.Sel.Name
				if slices.Contains(switchers, m) && !set[x] && !notify && !slices.Contains(callers[rel], m) {
					t.Errorf("%s: %s reaches %s; only the window's and the CLI's handlers may", fset.Position(x.Pos()), rel, m)
				}
				if h, ok := helpers[m]; ok && h.method && !slices.Contains(h.files, rel) {
					t.Errorf("%s: %s uses %s; only %s may", fset.Position(x.Pos()), rel, m, strings.Join(h.files, " and "))
				}
			case *ast.Ident:
				if h, ok := helpers[x.Name]; ok && !h.method && !slices.Contains(h.files, rel) {
					t.Errorf("%s: %s uses %s; only %s may", fset.Position(x.Pos()), rel, x.Name, strings.Join(h.files, " and "))
				}
			}
			return true
		})
		return nil
	}))
	if files < 50 {
		t.Fatalf("read only %d files: the walk is wrong", files)
	}
	// A helper renamed or moved would no longer be held: its new name
	// belongs in the list.
	for name, h := range helpers {
		if !declared[name] {
			t.Errorf("%s is no longer declared in %s; name what took its place in helpers", name, h.files[0])
		}
	}
}
