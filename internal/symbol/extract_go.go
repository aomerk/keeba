package symbol

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// goExtractor uses go/parser for accurate Go symbol extraction.
// Captures: package-level funcs, methods (with receiver), type decls,
// interface decls, and exported vars/consts.
type goExtractor struct{}

func (goExtractor) Extract(file string, src []byte) ([]Symbol, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var out []Symbol
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			out = append(out, goFuncSymbol(file, fset, d))
		case *ast.GenDecl:
			out = append(out, goGenSymbols(file, fset, d)...)
		}
	}
	return out, nil
}

func goFuncSymbol(file string, fset *token.FileSet, d *ast.FuncDecl) Symbol {
	kind := "function"
	receiver := ""
	if d.Recv != nil && len(d.Recv.List) > 0 {
		kind = "method"
		receiver = goReceiverTypeName(d.Recv.List[0].Type)
	}

	startPos := fset.Position(d.Pos())
	endPos := fset.Position(d.End())

	sig := goFuncSignature(file, fset, d)

	return Symbol{
		Name:      d.Name.Name,
		Kind:      kind,
		File:      file,
		StartLine: startPos.Line,
		EndLine:   endPos.Line,
		Signature: sig,
		Doc:       goDocText(d.Doc),
		Receiver:  receiver,
		Language:  "go",
	}
}

// goReceiverTypeName extracts "Server" from `(*Server)` or `(s *Server)`
// or `(s Server)` — handles pointer and value receivers and generics.
func goReceiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return goReceiverTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr: // Server[T]
		return goReceiverTypeName(t.X)
	case *ast.IndexListExpr: // Server[T, U]
		return goReceiverTypeName(t.X)
	case *ast.SelectorExpr: // pkg.Server
		return t.Sel.Name
	}
	return ""
}

// goFuncSignature returns the `func ...` opening line trimmed.
func goFuncSignature(_ string, fset *token.FileSet, d *ast.FuncDecl) string {
	startLine := fset.Position(d.Pos()).Line
	endLine := startLine
	if d.Body != nil {
		endLine = fset.Position(d.Body.Lbrace).Line
	}
	pf := fset.File(d.Pos())
	if pf == nil {
		return d.Name.Name
	}
	srcBytes := pf.Lines() // line offsets — not the file content; we don't have it here.
	_ = srcBytes
	_ = endLine
	// We don't have raw bytes from FileSet alone; reconstruct from the
	// positions and the AST instead. For the signature, the caller's src
	// is the canonical source; format from Name + Type with go/format.
	// To stay deterministic and dep-free, build a lightweight string.
	return goFuncSignatureFromAST(d)
}

// goFuncSignatureFromAST reconstructs a "func [Recv] Name(args) returns"
// line without invoking go/format (avoids the import cycle and saves
// bytes vs running format on each function).
func goFuncSignatureFromAST(d *ast.FuncDecl) string {
	var sb strings.Builder
	sb.WriteString("func ")
	if d.Recv != nil && len(d.Recv.List) > 0 {
		sb.WriteString("(")
		sb.WriteString(goExprString(d.Recv.List[0].Type))
		sb.WriteString(") ")
	}
	sb.WriteString(d.Name.Name)
	sb.WriteString("(")
	if d.Type.Params != nil {
		first := true
		for _, p := range d.Type.Params.List {
			if !first {
				sb.WriteString(", ")
			}
			first = false
			if len(p.Names) > 0 {
				names := make([]string, 0, len(p.Names))
				for _, n := range p.Names {
					names = append(names, n.Name)
				}
				sb.WriteString(strings.Join(names, ", "))
				sb.WriteString(" ")
			}
			sb.WriteString(goExprString(p.Type))
		}
	}
	sb.WriteString(")")
	if d.Type.Results != nil && len(d.Type.Results.List) > 0 {
		sb.WriteString(" ")
		multi := len(d.Type.Results.List) > 1 || (len(d.Type.Results.List) == 1 && len(d.Type.Results.List[0].Names) > 0)
		if multi {
			sb.WriteString("(")
		}
		first := true
		for _, r := range d.Type.Results.List {
			if !first {
				sb.WriteString(", ")
			}
			first = false
			if len(r.Names) > 0 {
				names := make([]string, 0, len(r.Names))
				for _, n := range r.Names {
					names = append(names, n.Name)
				}
				sb.WriteString(strings.Join(names, ", "))
				sb.WriteString(" ")
			}
			sb.WriteString(goExprString(r.Type))
		}
		if multi {
			sb.WriteString(")")
		}
	}
	return sb.String()
}

// goExprString is a small AST→string formatter for the type fragments
// that appear in signatures. Handles the common cases; fall through to a
// placeholder for exotic shapes.
func goExprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return goExprString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + goExprString(t.X)
	case *ast.ArrayType:
		return "[]" + goExprString(t.Elt)
	case *ast.Ellipsis:
		return "..." + goExprString(t.Elt)
	case *ast.MapType:
		return "map[" + goExprString(t.Key) + "]" + goExprString(t.Value)
	case *ast.ChanType:
		return "chan " + goExprString(t.Value)
	case *ast.FuncType:
		return "func(...)"
	case *ast.InterfaceType:
		return "interface{...}"
	case *ast.StructType:
		return "struct{...}"
	case *ast.IndexExpr:
		return goExprString(t.X) + "[" + goExprString(t.Index) + "]"
	case *ast.IndexListExpr:
		parts := make([]string, len(t.Indices))
		for i, ix := range t.Indices {
			parts[i] = goExprString(ix)
		}
		return goExprString(t.X) + "[" + strings.Join(parts, ", ") + "]"
	}
	return "..."
}

func goGenSymbols(file string, fset *token.FileSet, d *ast.GenDecl) []Symbol {
	var out []Symbol
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			kind := "type"
			if _, ok := s.Type.(*ast.InterfaceType); ok {
				kind = "interface"
			}
			out = append(out, Symbol{
				Name:      s.Name.Name,
				Kind:      kind,
				File:      file,
				StartLine: fset.Position(s.Pos()).Line,
				EndLine:   fset.Position(s.End()).Line,
				Signature: "type " + s.Name.Name + " " + goExprString(s.Type),
				Doc:       goDocText(d.Doc),
				Language:  "go",
			})
		case *ast.ValueSpec:
			kind := "var"
			if d.Tok == token.CONST {
				kind = "const"
			}
			for _, n := range s.Names {
				if !n.IsExported() {
					// Skip unexported; agents almost always ask about exported.
					continue
				}
				typeStr := ""
				if s.Type != nil {
					typeStr = " " + goExprString(s.Type)
				}
				out = append(out, Symbol{
					Name:      n.Name,
					Kind:      kind,
					File:      file,
					StartLine: fset.Position(s.Pos()).Line,
					EndLine:   fset.Position(s.End()).Line,
					Signature: kind + " " + n.Name + typeStr,
					Doc:       goDocText(d.Doc),
					Language:  "go",
				})
			}
		}
	}
	return out
}

func goDocText(g *ast.CommentGroup) string {
	if g == nil {
		return ""
	}
	return strings.TrimSpace(g.Text())
}
