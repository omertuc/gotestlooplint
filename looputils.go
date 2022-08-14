package gotestlooplint

import (
	"go/ast"

	"github.com/life4/genesis/slices"
)

func getLoopBody(loopNode ast.Node) *ast.BlockStmt {
	switch loopNode := loopNode.(type) {
	case *ast.ForStmt:
		return loopNode.Body
	case *ast.RangeStmt:
		return loopNode.Body
	default:
		panic("unexpected node type")
	}
}

func exprToIdent(expr ast.Expr) *ast.Ident { return expr.(*ast.Ident) }
func isNonNilExpr(expr ast.Expr) bool      { return expr != nil }

func getLoopVarsIdentifiers(loopNode ast.Node) []*ast.Ident {
	switch loopNode := loopNode.(type) {
	case *ast.ForStmt:
		// Get A, B, C, ... identifiers from `for A := ..., B := ..., var C ..., ...; ... ; ... { ... }`
		if loopAssignment, ok := loopNode.Init.(*ast.AssignStmt); ok {
			return slices.Map(loopAssignment.Lhs, exprToIdent)
		}
		return nil
	case *ast.RangeStmt:
		// Get A, B identifiers from `for A, B := range ... { ... }` or A from `for A := range ... { ... }`
		return slices.Map(slices.Filter([]ast.Expr{loopNode.Key, loopNode.Value}, isNonNilExpr), exprToIdent)
	default:
		panic("unexpected node type")
	}
}
