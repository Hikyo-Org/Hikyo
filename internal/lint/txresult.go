package lint

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// TxResult is the txresult analyzer.
var TxResult = &analysis.Analyzer{
	Name: "txresult",
	Doc:  "tx.WriteResult results must be detached data",
	Run: func(pass *analysis.Pass) (any, error) {
		if !inModule(pass.Pkg.Path()) {
			return nil, nil
		}
		for _, file := range pass.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || !isWriteResultCall(pass.TypesInfo, call.Fun) {
					return true
				}
				sig, _ := pass.TypesInfo.TypeOf(call.Fun).(*types.Signature)
				if sig == nil || sig.Results().Len() == 0 {
					return true
				}
				resultType := sig.Results().At(0).Type()
				if reason := detachedResultViolation(resultType, make(map[types.Type]bool)); reason != "" {
					pass.Reportf(call.Pos(), "tx.WriteResult result %s is not detached data: %s",
						types.TypeString(resultType, nil), reason)
				}
				return true
			})
		}
		return nil, nil
	},
}

func isWriteResultCall(info *types.Info, expr ast.Expr) bool {
	for {
		switch node := expr.(type) {
		case *ast.IndexExpr:
			expr = node.X
			continue
		case *ast.IndexListExpr:
			expr = node.X
			continue
		}
		break
	}
	var obj types.Object
	switch node := expr.(type) {
	case *ast.Ident:
		obj = info.Uses[node]
	case *ast.SelectorExpr:
		obj = info.Uses[node.Sel]
	}
	fn, ok := obj.(*types.Func)
	return ok && fn.Name() == "WriteResult" && fn.Pkg() != nil && fn.Pkg().Path() == Module+"/internal/store/tx"
}

func detachedResultViolation(t types.Type, seen map[types.Type]bool) string {
	t = types.Unalias(t)
	if seen[t] {
		return ""
	}
	seen[t] = true
	switch current := t.(type) {
	case *types.Named:
		obj := current.Obj()
		if obj.Pkg() != nil {
			path := obj.Pkg().Path()
			if (path == Module+"/internal/store" && (obj.Name() == "Repos" || obj.Name() == "ReadRepos")) ||
				(path == Module+"/internal/authz" && (obj.Name() == "Proof" || obj.Name() == "TxAuthorizer" || obj.Name() == "TxToken")) {
				return path + "." + obj.Name()
			}
		}
		return detachedResultViolation(current.Underlying(), seen)
	case *types.Pointer:
		return detachedResultViolation(current.Elem(), seen)
	case *types.Array:
		return detachedResultViolation(current.Elem(), seen)
	case *types.Slice:
		return detachedResultViolation(current.Elem(), seen)
	case *types.Map:
		if reason := detachedResultViolation(current.Key(), seen); reason != "" {
			return reason
		}
		return detachedResultViolation(current.Elem(), seen)
	case *types.Struct:
		for i := 0; i < current.NumFields(); i++ {
			if reason := detachedResultViolation(current.Field(i).Type(), seen); reason != "" {
				return reason
			}
		}
	case *types.Interface:
		return "interface values can retain attempt-owned authority"
	case *types.Signature:
		return "function values can capture attempt-owned authority"
	case *types.Chan:
		return "channels can transport attempt-owned authority"
	case *types.TypeParam:
		return "unresolved type parameter"
	case *types.Basic:
		if current.Kind() == types.UnsafePointer {
			return "unsafe.Pointer"
		}
	}
	return ""
}
