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
		(*ast.FuncDecl)(nil),
	}, func(functionNode ast.Node) {
		checkFunction(pass, functionNode.(*ast.FuncDecl))
	})

	return nil, nil
}

func isLoop(functionNode ast.Node) bool {
	switch functionNode.(type) {
	case *ast.ForStmt, *ast.RangeStmt:
		return true
	default:
		return false
	}
}

func checkFunction(pass *analysis.Pass, functionDeclaration *ast.FuncDecl) {
	if !strings.HasPrefix(functionDeclaration.Name.Name, "Test") {
		return
	}

	if functionDeclaration.Recv != nil {
		// A method that happens to be named Test<Something> is not a test
		return
	}

	testingTObject := pass.TypesInfo.ObjectOf(functionDeclaration.Type.Params.List[0].Names[0])

	// Look for loops inside the test function
	ast.Inspect(functionDeclaration.Body, func(functionBodyDescendantNode ast.Node) bool {
		if isLoop(functionBodyDescendantNode) {
			checkAndReportLoop(pass, functionBodyDescendantNode, testingTObject)
		}

		return true
	})
}

func isParallelFunctionClosure(pass *analysis.Pass, closure *ast.FuncLit) bool {
	// Closure test
	closureTestingTObject := pass.TypesInfo.ObjectOf(closure.Type.Params.List[0].Names[0])
	return findTestingTCalls(pass, closure.Body, closureTestingTObject, "Parallel") != nil
}

func checkAndReportLoop(pass *analysis.Pass, loopNode ast.Node, testingTObject types.Object) {
	loopVarsIdentifiersObjects := slices.Map(getLoopVarsIdentifiers(loopNode),
		pass.TypesInfo.ObjectOf)

	runCall := findTestingTCalls(pass, getLoopBody(loopNode), testingTObject, "Run")
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
func findTestingTCalls(pass *analysis.Pass, rootNode ast.Node,
	testingTObject types.Object, methodName string,
) *ast.CallExpr {
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
				if pass.TypesInfo.ObjectOf(callExpressionFunctionX) == testingTObject &&
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
