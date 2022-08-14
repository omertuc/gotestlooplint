package gotestlooplint

import (
	"go/ast"
	"go/types"
	"strings"

	"github.com/life4/genesis/slices"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var (
	goTestFailureMessageFormat = "loop variable `%s` used directly inside parallel test closure. This could lead to tests not running as expected. Try aliasing `%s` to a variable outside the closure"
	ginkgoFailureMessageFormat = "loop variable `%s` used directly inside ginkgo It closure. This could lead to tests not running as expected. Try aliasing `%s` to a variable outside the closure"
)

var Analyzer = &analysis.Analyzer{
	Name:     "gotestlooplint",
	Doc:      "gotestlooplint looks for loop var capture in parallel go tests or for loop var capture in regular Ginkgo tests",
	Run:      findIgnoredTests,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
}

func findIgnoredTests(pass *analysis.Pass) (interface{}, error) {
	pass.ResultOf[inspect.Analyzer].(*inspector.Inspector).Preorder([]ast.Node{
		(*ast.RangeStmt)(nil),
		(*ast.ForStmt)(nil),
	}, func(functionNode ast.Node) {
		checkAndReportLoop(pass, functionNode)
	})

	return nil, nil
}

func checkFunction(pass *analysis.Pass, functionDeclaration *ast.FuncDecl) {
	if !strings.HasPrefix(functionDeclaration.Name.Name, "Test") {
		return
	}

	if functionDeclaration.Recv != nil {
		// A method that happens to be named Test<Something> is not a test
		return
	}
}

func isParallelFunctionClosure(pass *analysis.Pass, closure *ast.FuncLit) bool {
	// Closure test
	return findTestingTCalls(pass, closure.Body, "Parallel") != nil
}

func checkAndReportLoop(pass *analysis.Pass, loopNode ast.Node) {
	loopVarsIdentifiersObjects := slices.Map(getLoopVarsIdentifiers(loopNode),
		pass.TypesInfo.ObjectOf)

	runCall := findTestingTCalls(pass, getLoopBody(loopNode), "Run")
	if runCall == nil {
		return
	}

	closure, ok := runCall.Args[1].(*ast.FuncLit)
	if !ok {
		return
	}

	// Check if this is a parallel closure
	if !isParallelFunctionClosure(pass, closure) {
		return
	}

	// Find all usages of the loop variables in the closure
	ast.Inspect(closure, func(closureDescendantNode ast.Node) bool {
		if identifier, ok := closureDescendantNode.(*ast.Ident); ok {
			// Compare against all loop variable objects
			identifierObject := pass.TypesInfo.ObjectOf(identifier)
			if slices.Any(loopVarsIdentifiersObjects, func(loopVarObject types.Object) bool {
				return identifierObject == loopVarObject
			}) {
				// Report if the identifier's object matches a loop var object
				name := identifier.Name
				pass.Reportf(identifier.Pos(), goTestFailureMessageFormat, name, name)
			}
		}

		return true
	})
}

// Scans a tree for method calls t.<methodName>() calls where t is the test context (t *testing.T)
func findTestingTCalls(pass *analysis.Pass, rootNode ast.Node, methodName string) *ast.CallExpr {
	var matchingCallExpression *ast.CallExpr

	ast.Inspect(rootNode, func(descendantNode ast.Node) bool {
		callExpression, ok := descendantNode.(*ast.CallExpr)
		if !ok {
			return true
		}

		switch callExpressionFunction := callExpression.Fun.(type) {
		case *ast.SelectorExpr:
			switch callExpressionFunctionX := callExpressionFunction.X.(type) {
			case *ast.Ident:
				if pass.TypesInfo.ObjectOf(callExpressionFunctionX).Type().String() == "*testing.T" &&
					callExpressionFunction.Sel.Name == methodName {
					matchingCallExpression = callExpression
					return false
				}
			}
		}

		return true
	})

	return matchingCallExpression
}
