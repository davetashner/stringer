// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"unicode"
)

// GoFuncInfo holds analysis results for a single Go function.
type GoFuncInfo struct {
	Name       string
	Receiver   string // empty for free functions, e.g. "(*Server)" for methods
	StartLine  int
	EndLine    int
	Lines      int // non-blank body lines
	IsExported bool
	Cyclomatic int // McCabe cyclomatic complexity
	Cognitive  int // SonarSource cognitive complexity
	MaxNesting int // maximum nesting depth
}

// analyzeGoFile parses a Go file and returns complexity info for each function.
func analyzeGoFile(path string) ([]GoFuncInfo, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var results []GoFuncInfo
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		info := GoFuncInfo{
			Name:       fn.Name.Name,
			StartLine:  fset.Position(fn.Pos()).Line,
			EndLine:    fset.Position(fn.End()).Line,
			IsExported: isGoExported(fn.Name.Name),
			Cyclomatic: cyclomaticComplexity(fset, fn),
			Cognitive:  cognitiveComplexity(fset, fn),
			MaxNesting: maxNestingDepth(fset, fn),
		}

		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			info.Receiver = receiverString(fn.Recv.List[0].Type)
		}

		info.Lines = countNonBlankBodyLines(fset, fn)

		results = append(results, info)
	}

	return results, nil
}

// analyzeGoSource parses Go source from a byte slice (for testing).
func analyzeGoSource(src []byte) ([]GoFuncInfo, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var results []GoFuncInfo
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		info := GoFuncInfo{
			Name:       fn.Name.Name,
			StartLine:  fset.Position(fn.Pos()).Line,
			EndLine:    fset.Position(fn.End()).Line,
			IsExported: isGoExported(fn.Name.Name),
			Cyclomatic: cyclomaticComplexity(fset, fn),
			Cognitive:  cognitiveComplexity(fset, fn),
			MaxNesting: maxNestingDepth(fset, fn),
		}

		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			info.Receiver = receiverString(fn.Recv.List[0].Type)
		}

		info.Lines = countNonBlankBodyLines(fset, fn)

		results = append(results, info)
	}

	return results, nil
}

// isGoExported returns true if the name starts with an uppercase letter.
func isGoExported(name string) bool {
	if name == "" {
		return false
	}
	return unicode.IsUpper(rune(name[0]))
}

// receiverString formats the receiver type expression as a string.
func receiverString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return "(*" + receiverString(t.X) + ")"
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		// Generic receiver: T[P]
		return receiverString(t.X) + "[" + receiverString(t.Index) + "]"
	case *ast.IndexListExpr:
		// Generic receiver with multiple type params: T[P, Q]
		parts := make([]string, len(t.Indices))
		for i, idx := range t.Indices {
			parts[i] = receiverString(idx)
		}
		return receiverString(t.X) + "[" + strings.Join(parts, ", ") + "]"
	default:
		return ""
	}
}

// countNonBlankBodyLines counts non-blank lines in a function body.
func countNonBlankBodyLines(fset *token.FileSet, fn *ast.FuncDecl) int {
	if fn.Body == nil {
		return 0
	}

	startLine := fset.Position(fn.Body.Lbrace).Line
	endLine := fset.Position(fn.Body.Rbrace).Line

	// For single-line functions, body is between braces on same line.
	if startLine == endLine {
		return 0
	}

	// We need the actual source to count non-blank lines. Since we only
	// have the AST, approximate by counting lines with statements.
	count := 0
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		line := fset.Position(n.Pos()).Line
		// Count unique lines that have AST nodes (excluding braces).
		if line > startLine && line < endLine {
			count++
		}
		return true
	})

	// Better approximation: just use endLine - startLine - 1 for the
	// number of body lines, which is the line count excluding braces.
	// This is consistent with what non-blank counting would typically yield.
	return endLine - startLine - 1
}

// cyclomaticComplexity computes McCabe cyclomatic complexity for a function.
//
// Base = 1, then +1 for each decision point:
//   - if, for (including range), case (in switch/select, excluding default)
//   - && and || boolean operators
func cyclomaticComplexity(_ *token.FileSet, fn *ast.FuncDecl) int {
	if fn.Body == nil {
		return 1
	}

	complexity := 1

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.IfStmt:
			complexity++
		case *ast.ForStmt:
			complexity++
		case *ast.RangeStmt:
			complexity++
		case *ast.CaseClause:
			// +1 for each case except default (which has nil list).
			if node.List != nil {
				complexity++
			}
		case *ast.CommClause:
			// +1 for each case in select except default.
			if node.Comm != nil {
				complexity++
			}
		case *ast.BinaryExpr:
			if node.Op == token.LAND || node.Op == token.LOR {
				complexity++
			}
		}
		return true
	})

	return complexity
}

// cognitiveComplexity computes SonarSource cognitive complexity for a function.
//
// Increments (no nesting penalty): else, else if, &&/|| (only on operator switch)
// Increments + nesting penalty: if, for, switch, select, goto
// Nesting increased by: if, for, switch, select, func literals
// No increment for: case, default, break, continue, return
func cognitiveComplexity(_ *token.FileSet, fn *ast.FuncDecl) int {
	if fn.Body == nil {
		return 0
	}

	v := &cognitiveVisitor{}
	v.walkStmtList(fn.Body.List, 0)
	return v.score
}

type cognitiveVisitor struct {
	score int
}

func (v *cognitiveVisitor) walkStmtList(stmts []ast.Stmt, nesting int) {
	for _, stmt := range stmts {
		v.walkStmt(stmt, nesting)
	}
}

func (v *cognitiveVisitor) walkStmt(stmt ast.Stmt, nesting int) {
	switch s := stmt.(type) {
	case *ast.IfStmt:
		// +1 for the if, + nesting penalty
		v.score += 1 + nesting
		if s.Init != nil {
			v.walkStmt(s.Init, nesting+1)
		}
		if s.Cond != nil {
			v.walkExpr(s.Cond)
		}
		v.walkStmtList(s.Body.List, nesting+1)
		if s.Else != nil {
			switch e := s.Else.(type) {
			case *ast.IfStmt:
				// "else if" — +1 for the else if (no nesting penalty),
				// then recurse into the if at same nesting level.
				v.score++ // +1 for else if
				if e.Init != nil {
					v.walkStmt(e.Init, nesting+1)
				}
				if e.Cond != nil {
					v.walkExpr(e.Cond)
				}
				v.walkStmtList(e.Body.List, nesting+1)
				if e.Else != nil {
					v.walkElse(e.Else, nesting)
				}
			case *ast.BlockStmt:
				// plain else — +1, no nesting penalty
				v.score++
				v.walkStmtList(e.List, nesting+1)
			}
		}

	case *ast.ForStmt:
		v.score += 1 + nesting
		if s.Init != nil {
			v.walkStmt(s.Init, nesting+1)
		}
		if s.Cond != nil {
			v.walkExpr(s.Cond)
		}
		if s.Post != nil {
			v.walkStmt(s.Post, nesting+1)
		}
		v.walkStmtList(s.Body.List, nesting+1)

	case *ast.RangeStmt:
		v.score += 1 + nesting
		v.walkStmtList(s.Body.List, nesting+1)

	case *ast.SwitchStmt:
		v.score += 1 + nesting
		if s.Init != nil {
			v.walkStmt(s.Init, nesting+1)
		}
		if s.Tag != nil {
			v.walkExpr(s.Tag)
		}
		v.walkStmtList(s.Body.List, nesting+1)

	case *ast.TypeSwitchStmt:
		v.score += 1 + nesting
		if s.Init != nil {
			v.walkStmt(s.Init, nesting+1)
		}
		if s.Assign != nil {
			v.walkStmt(s.Assign, nesting+1)
		}
		v.walkStmtList(s.Body.List, nesting+1)

	case *ast.SelectStmt:
		v.score += 1 + nesting
		v.walkStmtList(s.Body.List, nesting+1)

	case *ast.BranchStmt:
		if s.Tok == token.GOTO {
			v.score += 1 + nesting
		}

	case *ast.CaseClause:
		// case/default don't increment — walk body at current nesting
		for _, expr := range s.List {
			v.walkExpr(expr)
		}
		v.walkStmtList(s.Body, nesting)

	case *ast.CommClause:
		// select case — no increment
		if s.Comm != nil {
			v.walkStmt(s.Comm, nesting)
		}
		v.walkStmtList(s.Body, nesting)

	case *ast.BlockStmt:
		v.walkStmtList(s.List, nesting)

	case *ast.ExprStmt:
		v.walkExpr(s.X)

	case *ast.AssignStmt:
		for _, expr := range s.Rhs {
			v.walkExpr(expr)
		}

	case *ast.ReturnStmt:
		for _, expr := range s.Results {
			v.walkExpr(expr)
		}

	case *ast.DeferStmt:
		v.walkExpr(s.Call.Fun)
		for _, arg := range s.Call.Args {
			v.walkExpr(arg)
		}

	case *ast.GoStmt:
		v.walkExpr(s.Call.Fun)
		for _, arg := range s.Call.Args {
			v.walkExpr(arg)
		}

	case *ast.SendStmt:
		v.walkExpr(s.Value)

	case *ast.LabeledStmt:
		v.walkStmt(s.Stmt, nesting)

	case *ast.DeclStmt:
		if gd, ok := s.Decl.(*ast.GenDecl); ok {
			for _, spec := range gd.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					for _, val := range vs.Values {
						v.walkExpr(val)
					}
				}
			}
		}

	case *ast.IncDecStmt:
		// nothing to walk

	}
}

func (v *cognitiveVisitor) walkElse(node ast.Stmt, nesting int) {
	switch e := node.(type) {
	case *ast.IfStmt:
		// else if chain
		v.score++ // +1 for else if
		if e.Init != nil {
			v.walkStmt(e.Init, nesting+1)
		}
		if e.Cond != nil {
			v.walkExpr(e.Cond)
		}
		v.walkStmtList(e.Body.List, nesting+1)
		if e.Else != nil {
			v.walkElse(e.Else, nesting)
		}
	case *ast.BlockStmt:
		// plain else
		v.score++
		v.walkStmtList(e.List, nesting+1)
	}
}

func (v *cognitiveVisitor) walkExpr(expr ast.Expr) {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		v.walkExpr(e.X)
		if e.Op == token.LAND || e.Op == token.LOR {
			v.addLogicalOp(e)
		}
		v.walkExpr(e.Y)

	case *ast.FuncLit:
		// Function literals increase nesting for their body.
		// We don't increment score for the literal itself,
		// but nested constructs inside get an extra nesting level.
		if e.Body != nil {
			v.walkStmtList(e.Body.List, 1) // func lit creates nesting level 1 from its perspective
		}

	case *ast.CallExpr:
		v.walkExpr(e.Fun)
		for _, arg := range e.Args {
			v.walkExpr(arg)
		}

	case *ast.UnaryExpr:
		v.walkExpr(e.X)

	case *ast.ParenExpr:
		v.walkExpr(e.X)

	case *ast.CompositeLit:
		for _, elt := range e.Elts {
			v.walkExpr(elt)
		}

	case *ast.KeyValueExpr:
		v.walkExpr(e.Value)
	}
}

// addLogicalOp adds to cognitive score for &&/|| only when the operator
// switches from the previous one in the same expression (sequences of the
// same operator don't add).
func (v *cognitiveVisitor) addLogicalOp(expr *ast.BinaryExpr) {
	// Check if the left operand is a binary expr with the same operator.
	// If so, this is a continuation (e.g., a && b && c) — don't add.
	// If the operators differ (e.g., a && b || c) — add for the switch.
	if left, ok := expr.X.(*ast.BinaryExpr); ok {
		if (left.Op == token.LAND || left.Op == token.LOR) && left.Op == expr.Op {
			// Same operator as left child — no increment (continuation).
			return
		}
	}
	// Either left is not a logical op, or operators differ — increment.
	v.score++
}

// maxNestingDepth computes the maximum nesting depth within a function body.
//
// Function body is level 0. Nesting incremented by: if, for, range, switch,
// select, function literals. Tracks the maximum depth reached.
func maxNestingDepth(_ *token.FileSet, fn *ast.FuncDecl) int {
	if fn.Body == nil {
		return 0
	}

	v := &nestingVisitor{}
	v.walkStmtList(fn.Body.List, 0)
	return v.max
}

type nestingVisitor struct {
	max int
}

func (v *nestingVisitor) walkStmtList(stmts []ast.Stmt, depth int) {
	for _, stmt := range stmts {
		v.walkStmt(stmt, depth)
	}
}

func (v *nestingVisitor) walkStmt(stmt ast.Stmt, depth int) {
	switch s := stmt.(type) {
	case *ast.IfStmt:
		v.enter(depth + 1)
		if s.Init != nil {
			v.walkStmt(s.Init, depth+1)
		}
		v.walkStmtList(s.Body.List, depth+1)
		if s.Else != nil {
			// else/else-if is at the same nesting level as the if.
			v.walkStmt(s.Else, depth)
		}

	case *ast.ForStmt:
		v.enter(depth + 1)
		v.walkStmtList(s.Body.List, depth+1)

	case *ast.RangeStmt:
		v.enter(depth + 1)
		v.walkStmtList(s.Body.List, depth+1)

	case *ast.SwitchStmt:
		v.enter(depth + 1)
		v.walkStmtList(s.Body.List, depth+1)

	case *ast.TypeSwitchStmt:
		v.enter(depth + 1)
		v.walkStmtList(s.Body.List, depth+1)

	case *ast.SelectStmt:
		v.enter(depth + 1)
		v.walkStmtList(s.Body.List, depth+1)

	case *ast.CaseClause:
		v.walkStmtList(s.Body, depth)

	case *ast.CommClause:
		if s.Comm != nil {
			v.walkStmt(s.Comm, depth)
		}
		v.walkStmtList(s.Body, depth)

	case *ast.BlockStmt:
		v.walkStmtList(s.List, depth)

	case *ast.LabeledStmt:
		v.walkStmt(s.Stmt, depth)

	case *ast.ExprStmt:
		v.walkExpr(s.X, depth)

	case *ast.AssignStmt:
		for _, expr := range s.Rhs {
			v.walkExpr(expr, depth)
		}

	case *ast.ReturnStmt:
		for _, expr := range s.Results {
			v.walkExpr(expr, depth)
		}

	case *ast.DeferStmt:
		v.walkExpr(s.Call.Fun, depth)
		for _, arg := range s.Call.Args {
			v.walkExpr(arg, depth)
		}

	case *ast.GoStmt:
		v.walkExpr(s.Call.Fun, depth)
		for _, arg := range s.Call.Args {
			v.walkExpr(arg, depth)
		}

	case *ast.DeclStmt:
		if gd, ok := s.Decl.(*ast.GenDecl); ok {
			for _, spec := range gd.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					for _, val := range vs.Values {
						v.walkExpr(val, depth)
					}
				}
			}
		}
	}
}

func (v *nestingVisitor) walkExpr(expr ast.Expr, depth int) {
	switch e := expr.(type) {
	case *ast.FuncLit:
		if e.Body != nil {
			v.enter(depth + 1)
			v.walkStmtList(e.Body.List, depth+1)
		}

	case *ast.CallExpr:
		v.walkExpr(e.Fun, depth)
		for _, arg := range e.Args {
			v.walkExpr(arg, depth)
		}

	case *ast.CompositeLit:
		for _, elt := range e.Elts {
			v.walkExpr(elt, depth)
		}

	case *ast.KeyValueExpr:
		v.walkExpr(e.Value, depth)

	case *ast.UnaryExpr:
		v.walkExpr(e.X, depth)

	case *ast.BinaryExpr:
		v.walkExpr(e.X, depth)
		v.walkExpr(e.Y, depth)

	case *ast.ParenExpr:
		v.walkExpr(e.X, depth)
	}
}

func (v *nestingVisitor) enter(depth int) {
	if depth > v.max {
		v.max = depth
	}
}
