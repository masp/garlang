// Package compiler emits Core Erlang from the AST generated by the parser.
package compile

import (
	"fmt"
	"strings"

	"github.com/masp/garlang/ast"
	"github.com/masp/garlang/core"
	"github.com/masp/garlang/parse"
)

type Environment struct {
	Variables map[string]core.Var
}

type Compiler struct {
	errors []error
}

func New() *Compiler {
	return &Compiler{}
}

func (c *Compiler) CompileModule(mod *ast.Module) (*core.Module, error) {
	mod = addBaseFuncs(mod)
	return c.compileModule(mod)
}

// compileModule compiles a module AST into a Core Erlang module.
func (c *Compiler) compileModule(mod *ast.Module) (*core.Module, error) {
	coreMod := &core.Module{
		Name: mod.Id.Name,
	}

	for _, decl := range mod.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			coreFn, err := c.compileFunction(d)
			if err != nil {
				return coreMod, err
			}
			if d.IsPublic() {
				coreMod.Exports = append(coreMod.Exports, coreFn.Name)
			}
			coreMod.Functions = append(coreMod.Functions, coreFn)
		default:
			panic(fmt.Errorf("unrecognized decl: %T", decl))
		}
	}
	return coreMod, nil
}

func (c *Compiler) CompileFunction(fn *ast.FuncDecl) (core.Func, error) {
	return c.compileFunction(fn)
}

func (c *Compiler) compileFunction(fn *ast.FuncDecl) (core.Func, error) {
	coreFn := core.Func{
		Name: core.FuncName{Name: fn.Name.Name, Arity: len(fn.Parameters)},
		Annotation: core.Annotation{Attrs: []core.Const{
			core.ConstTuple{Elements: []core.Const{
				core.Atom{Value: "function"},
				core.ConstTuple{
					Elements: []core.Const{core.Atom{Value: fn.Name.Name}, core.Integer{Value: int64(len(fn.Parameters))}},
				},
			}},
		}},
	}

	for _, arg := range fn.Parameters {
		coreFn.Parameters = append(coreFn.Parameters, core.Var{Name: arg.Name})
	}

	var err error
	coreFn.Body, err = c.compileStatements(fn.Statements)
	return coreFn, err
}

func (c *Compiler) compileStatements(stmts []ast.Statement) (core.Expr, error) {
	var expr core.Expr
	for _, stmt := range stmts {
		switch stmt := stmt.(type) {
		case *ast.ReturnStatement:
			expr = c.compileExpr(stmt.Expression)
		}
	}
	return expr, nil
}

func (c *Compiler) compileExprs(exprs []ast.Expression) []core.Expr {
	var coreExprs []core.Expr
	for _, expr := range exprs {
		coreExprs = append(coreExprs, c.compileExpr(expr))
	}
	return coreExprs
}

func (c *Compiler) compileExpr(expr ast.Expression) core.Expr {
	switch expr := expr.(type) {
	case *ast.IntLiteral:
		return core.Integer{Value: expr.Value}
	case *ast.StringLiteral:
		return core.String{Value: expr.Value}
	case *ast.Identifier:
		return core.Var{Name: expr.Name}
	case *ast.AtomLiteral:
		return core.Atom{Value: expr.Value}
	case *ast.CallExpr:
		return c.compileCallExpr(expr)
	default:
		panic(fmt.Errorf("unrecognized expression type: %T", expr))
	}
}

func (c *Compiler) compileCallExpr(call *ast.CallExpr) core.Expr {
	switch expr := call.Callee.(type) {
	case *ast.DotExpr:
		return c.compileDotCallExpr(call, expr)
	default: // local function, we can validate that the function exists
		return c.compileLocalCallExpr(call)
	}
}

func (c *Compiler) compileLocalCallExpr(expr *ast.CallExpr) core.Expr {
	// If an identifier and identifier is not defined in function as variable,
	// treat as an atom
	if ident, ok := expr.Callee.(*ast.Identifier); ok {
		expr.Callee = &ast.AtomLiteral{Value: ident.Name}
	}

	return core.Application{
		Func: c.compileExpr(expr.Callee),
		Args: c.compileExprs(expr.Arguments),
	}
}

func (c *Compiler) compileDotCallExpr(call *ast.CallExpr, dot *ast.DotExpr) core.Expr {
	// If an identifier and identifier is not defined in function as variable,
	// treat as an atom
	if ident, ok := dot.Target.(*ast.Identifier); ok {
		dot.Target = &ast.AtomLiteral{Value: ident.Name}
	}
	return core.InterModuleCall{
		Module: c.compileExpr(dot.Target),
		Func:   core.Atom{Value: dot.Attribute.Name},
		Args:   c.compileExprs(call.Arguments),
	}
}

// commonModFuncs are default funcs that are included in every Erlang module
// If these are not included, the Erlang VM will not be able to load the module.
func commonModFuncs(mod *ast.Module) string {
	return strings.NewReplacer("{{mod}}", mod.Id.Name).Replace(`
module common

func module_info() {
	return erlang.module_info('{{mod}}')
}

func module_info(Value) {
	return erlang.module_info('{{mod}}', Value)
}
`)
}

// addBaseFuncs adds the module_info functions that Erlang requires as part of every
// module.
//
// The functions are very simple: just call 'erlang':module_info/1 with the appropriate atom.
func addBaseFuncs(mod *ast.Module) *ast.Module {
	commonMod, err := parse.Module("<builtin>", []byte(commonModFuncs(mod)))
	if err != nil {
		panic(err)
	}
	mod.Decls = append(commonMod.Decls, mod.Decls...)
	return mod
}
