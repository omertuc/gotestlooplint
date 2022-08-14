package gotestlooplint

import (
	"go/ast"
	"go/token"
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
	}, func(loopNode ast.Node) {
		// recover panic
		defer func() {
			if r := recover(); r != nil {
				pass.Reportf(loopNode.Pos(), "panic: %s\n", r)
			}
		}()

		checkAndReportLoop(pass, loopNode)
		checkAndReportLoopGinkgo(pass, loopNode)
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

func isParallelFunctionClosure(pass *analysis.Pass, closure *ast.FuncLit) *token.Pos {
	// Closure test
	if parallelCall := findTestingTCalls(pass, closure.Body, "Parallel"); parallelCall != nil {
		parallelCallPos := parallelCall.Pos()
		return &parallelCallPos
	}

	return nil
}

func getLoopNodeIdentifiersObjects(pass *analysis.Pass, loopNode ast.Node) []types.Object {
	return slices.Map(getLoopVarsIdentifiers(loopNode), pass.TypesInfo.ObjectOf)
}

func checkAndReportLoop(pass *analysis.Pass, loopNode ast.Node) {
	loopVarsIdentifiersObjects := getLoopNodeIdentifiersObjects(pass, loopNode)

	runCall := findTestingTCalls(pass, getLoopBody(loopNode), "Run")
	if runCall == nil {
		return
	}

	closure := getSubtestClosure(runCall)
	if closure == nil {
		return
	}

	// Check if this is a parallel closure
	parallelTokenPos := isParallelFunctionClosure(pass, closure)
	if parallelTokenPos == nil {
		return
	}

	// Find all usages of the loop variables in the closure
	ast.Inspect(closure, func(closureDescendantNode ast.Node) bool {
		if closureDescendantNode == nil {
			return true
		}

		if closureDescendantNode.Pos() <= *parallelTokenPos {
			// This identifier is before the parallel token, so it is allowed to be used in the closure
			return true
		}

		return checkAndReportLoopIdentifierObject(pass, loopVarsIdentifiersObjects, closureDescendantNode, goTestFailureMessageFormat)
	})
}

func checkAndReportLoopGinkgo(pass *analysis.Pass, loopNode ast.Node) {
	loopVarsIdentifiersObjects := getLoopNodeIdentifiersObjects(pass, loopNode)

	ginkgoItCall := findGinkgoItCalls(pass, getLoopBody(loopNode))
	if ginkgoItCall == nil {
		return
	}

	closure := getSubtestClosure(ginkgoItCall)
	if closure == nil {
		return
	}

	// Find all usages of the loop variables in the closure
	ast.Inspect(closure, func(closureDescendantNode ast.Node) bool {
		return checkAndReportLoopIdentifierObject(pass, loopVarsIdentifiersObjects, closureDescendantNode, ginkgoFailureMessageFormat)
	})
}

func getSubtestClosure(runCall *ast.CallExpr) *ast.FuncLit {
	closure, ok := runCall.Args[1].(*ast.FuncLit)
	if !ok {
		return nil
	}
	return closure
}

func checkAndReportLoopIdentifierObject(pass *analysis.Pass, loopVarsIdentifiersObjects []types.Object, node ast.Node, message string) bool {
	if identifier, ok := node.(*ast.Ident); ok {
		// Compare against all loop variable objects
		identifierObject := pass.TypesInfo.ObjectOf(identifier)

		if slices.Any(loopVarsIdentifiersObjects, func(loopVarObject types.Object) bool {
			return identifierObject == loopVarObject
		}) {
			name := identifier.Name
			pass.Reportf(identifier.Pos(), message, name, name)
			return false
		}
	}

	return true
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

// Scans a tree for Ginkgo It method calls
func findGinkgoItCalls(pass *analysis.Pass, rootNode ast.Node) *ast.CallExpr {
	var matchingCallExpression *ast.CallExpr

	ast.Inspect(rootNode, func(descendantNode ast.Node) bool {
		callExpression, ok := descendantNode.(*ast.CallExpr)
		if !ok {
			return true
		}

		var callIdentifier *ast.Ident
		switch callExpressionFunction := callExpression.Fun.(type) {
		case *ast.SelectorExpr:
			// This is when ginkgo is imported regularly, i.e. the call looks something like `ginkgo.It`
			callIdentifier = callExpressionFunction.Sel
		case *ast.Ident:
			// This is when ginkgo is imported as wildcard, i.e. the call looks something like `It`
			callIdentifier = callExpressionFunction
		default:
			return true
		}

		if callIdentifier != nil && callIdentifier.Name == "It" && isGinkgoIdentifier(pass, callIdentifier) {
			matchingCallExpression = callExpression
			return false
		}

		return true
	})

	return matchingCallExpression
}

func isGinkgoIdentifier(pass *analysis.Pass, identifier *ast.Ident) bool {
	packagePath := pass.TypesInfo.ObjectOf(identifier).Pkg().Path()
	return packagePath == "github.com/onsi/ginkgo/v2" || packagePath == "github.com/onsi/ginkgo"
}
