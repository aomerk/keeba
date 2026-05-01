package symbol

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// RefEdge is one (caller → callee) link where the callee is referenced
// in a position OTHER than CallExpr.Fun. find_callers covers calls;
// find_refs covers everything else: type positions, struct field types,
// composite literal types, type assertions, embedded fields. Pairs with
// CallEdge to give the agent a full impact picture for "rename this
// type" questions that calls alone don't answer.
//
// Kind ∈ {"type", "embed"} in v1. "value" / "field" refs may follow in
// v2 once we have a real need; emitting them today would be noisy
// (every bare ident in a function body) without a way to disambiguate
// local vars from package-level symbols without go/types.
type RefEdge struct {
	Caller     string `json:"caller"`
	CallerFile string `json:"caller_file"`
	CallerLine int    `json:"caller_line"`
	Callee     string `json:"callee"`
	Kind       string `json:"kind"`
}

// ExtractRefs returns every type / embed reference in src. Go-only for
// v1 — non-Go languages return nil. Unparseable Go also returns nil so
// per-file errors don't poison the whole compile walk.
func ExtractRefs(file string, src []byte) []RefEdge {
	if filepath.Ext(file) != ".go" {
		return nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		return nil
	}

	r := &refWalker{file: file, fset: fset}
	r.walkFile(f)
	return r.edges
}

// refWalker accumulates ref edges while walking the file's decls. The
// enclosing-symbol stack lets us attribute each ref to the right caller
// — a type ref in a method body belongs to the method, not the file.
type refWalker struct {
	file  string
	fset  *token.FileSet
	stack []string
	edges []RefEdge
}

func (r *refWalker) push(name string) { r.stack = append(r.stack, name) }
func (r *refWalker) pop()             { r.stack = r.stack[:len(r.stack)-1] }
func (r *refWalker) caller() string {
	if len(r.stack) == 0 {
		return "<file>"
	}
	return r.stack[len(r.stack)-1]
}

// emit records one ref. Skips "the declaration's own name registers as
// a ref to itself" by ignoring identifiers whose line matches the
// caller's defining line — see comment on isSelfDecl below.
func (r *refWalker) emit(node ast.Node, callee, kind string) {
	if callee == "" {
		return
	}
	r.edges = append(r.edges, RefEdge{
		Caller:     r.caller(),
		CallerFile: r.file,
		CallerLine: r.fset.Position(node.Pos()).Line,
		Callee:     callee,
		Kind:       kind,
	})
}

// walkFile dispatches each top-level declaration to its specialized walker.
func (r *refWalker) walkFile(f *ast.File) {
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			r.walkGenDecl(d)
		case *ast.FuncDecl:
			r.walkFuncDecl(d)
		}
	}
}

// walkGenDecl handles type / var / const declarations at the file
// level. TypeSpec drives most of the embed-ref work.
func (r *refWalker) walkGenDecl(d *ast.GenDecl) {
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			r.push(s.Name.Name)
			r.walkTypeSpec(s)
			r.pop()
		case *ast.ValueSpec:
			// var / const at file level. Only the type expression matters
			// for refs; expression initializers we leave to walkExpr below
			// so we catch composite-lit type refs in initializers too.
			if s.Type != nil {
				r.walkType(s.Type)
			}
			for _, v := range s.Values {
				r.walkExpr(v)
			}
		}
	}
}

// walkTypeSpec splits between struct / interface (where we look for
// embedded fields and field types) and aliases (where the right-hand
// side is itself a type expression).
func (r *refWalker) walkTypeSpec(s *ast.TypeSpec) {
	switch t := s.Type.(type) {
	case *ast.StructType:
		r.walkStructFields(t.Fields, false)
	case *ast.InterfaceType:
		r.walkStructFields(t.Methods, true)
	default:
		// type X = Y / type X Y / type X []Foo, etc.
		r.walkType(s.Type)
	}
}

// walkStructFields handles a list of fields where unnamed entries are
// embedded — that's structs and interfaces. Use walkTypeFields for
// receiver / params / results, where unnamed entries are anonymous
// types (not embeds).
func (r *refWalker) walkStructFields(fl *ast.FieldList, _ bool) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		if len(f.Names) == 0 {
			if name := typeIdentName(f.Type); name != "" {
				r.emit(f, name, "embed")
			}
			continue
		}
		r.walkType(f.Type)
	}
}

// walkTypeFields handles a list of fields where every entry's Type is
// a type ref regardless of whether names are present — receiver / params
// / results / function-type signatures. `func() (int, error)` has two
// unnamed result fields; both are type refs, not embeds.
func (r *refWalker) walkTypeFields(fl *ast.FieldList) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		r.walkType(f.Type)
	}
}

// walkFuncDecl handles function and method declarations. The receiver,
// params, and return types all sit in *ast.FieldList.Type positions —
// each one is a "type" ref. The body is walked for nested type refs
// (composite literals, type assertions, var declarations).
func (r *refWalker) walkFuncDecl(d *ast.FuncDecl) {
	r.push(d.Name.Name)
	defer r.pop()

	if d.Recv != nil {
		r.walkTypeFields(d.Recv)
	}
	if d.Type != nil {
		r.walkTypeFields(d.Type.Params)
		r.walkTypeFields(d.Type.Results)
	}
	if d.Body != nil {
		r.walkExpr(d.Body)
	}
}

// walkType walks a type expression and emits one "type" ref per Ident
// terminal (including the rightmost in a SelectorExpr like pkg.Foo).
// Recurses through pointer / slice / array / map / chan / func types.
func (r *refWalker) walkType(e ast.Expr) {
	if e == nil {
		return
	}
	switch t := e.(type) {
	case *ast.Ident:
		if isGoTypeBuiltin(t.Name) {
			return
		}
		r.emit(t, t.Name, "type")
	case *ast.SelectorExpr:
		// pkg.Foo — emit Foo as a type ref. The package qualifier is
		// dropped (we don't have package info), matching the find_callers
		// "drop package selector base" pattern.
		r.emit(t.Sel, t.Sel.Name, "type")
	case *ast.StarExpr:
		r.walkType(t.X)
	case *ast.ArrayType:
		r.walkType(t.Elt)
	case *ast.MapType:
		r.walkType(t.Key)
		r.walkType(t.Value)
	case *ast.ChanType:
		r.walkType(t.Value)
	case *ast.FuncType:
		r.walkTypeFields(t.Params)
		r.walkTypeFields(t.Results)
	case *ast.StructType:
		r.walkStructFields(t.Fields, false)
	case *ast.InterfaceType:
		r.walkStructFields(t.Methods, true)
	case *ast.IndexExpr: // generic Foo[T]
		r.walkType(t.X)
		r.walkType(t.Index)
	case *ast.IndexListExpr:
		r.walkType(t.X)
		for _, idx := range t.Indices {
			r.walkType(idx)
		}
	case *ast.ParenExpr:
		r.walkType(t.X)
	}
}

// walkExpr walks an expression looking for embedded TYPE positions —
// CompositeLit.Type and TypeAssertExpr.Type are the two that matter.
// Recurses through statements / expressions but does NOT emit "value"
// refs for bare idents (v1 keeps the surface tight; see RefEdge doc).
func (r *refWalker) walkExpr(node ast.Node) {
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if x.Type != nil {
				r.walkType(x.Type)
			}
		case *ast.TypeAssertExpr:
			if x.Type != nil {
				r.walkType(x.Type)
			}
		case *ast.ValueSpec: // var x Foo inside a function body
			if x.Type != nil {
				r.walkType(x.Type)
			}
		case *ast.FuncLit:
			// Closure types — params/returns count as type refs in the
			// enclosing function.
			if x.Type != nil {
				r.walkTypeFields(x.Type.Params)
				r.walkTypeFields(x.Type.Results)
			}
		}
		return true
	})
}

// typeIdentName returns the rightmost identifier of a type expression —
// the bare name we key the inverse index on. Mirrors goCallName in
// calls.go but for type positions (no IndexExpr unwrap needed at the
// embed-name level).
func typeIdentName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.StarExpr:
		return typeIdentName(t.X)
	case *ast.IndexExpr:
		return typeIdentName(t.X)
	case *ast.IndexListExpr:
		return typeIdentName(t.X)
	}
	return ""
}

// isGoTypeBuiltin filters out the predeclared type names so refs don't
// flood with "string", "int", "error" noise. Mirrors the call-builtins
// list but for type position.
func isGoTypeBuiltin(name string) bool {
	switch name {
	case "bool", "byte", "complex64", "complex128",
		"error", "float32", "float64",
		"int", "int8", "int16", "int32", "int64",
		"rune", "string",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"any", "comparable":
		return true
	}
	return false
}

// ExtractRefsRepo is the file-walking equivalent of ExtractCallsRepo /
// ExtractRepo. Used by symbol.Compile to populate Index.Refs.
func ExtractRefsRepo(repoRoot string) ([]RefEdge, error) {
	const maxFileBytes = 1 << 20

	skip := map[string]struct{}{
		".git": {}, ".hg": {}, ".svn": {},
		"node_modules": {}, "vendor": {}, ".venv": {}, "venv": {}, "env": {},
		"__pycache__": {}, ".tox": {}, ".pytest_cache": {}, ".mypy_cache": {},
		".ruff_cache": {}, "dist": {}, "build": {}, ".next": {}, ".nuxt": {},
		"target": {}, ".idea": {}, ".vscode": {}, ".keeba": {}, ".cache": {},
	}

	var out []RefEdge
	err := filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if _, drop := skip[name]; drop {
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") && path != repoRoot {
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err == nil && info.Size() > maxFileBytes {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		src, err := os.ReadFile(path) //nolint:gosec
		if err != nil {
			return nil
		}
		if refs := ExtractRefs(rel, src); len(refs) > 0 {
			out = append(out, refs...)
		}
		return nil
	})
	return out, err
}
