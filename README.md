# FxForce5

Attempt to use [AST-based](https://pkg.go.dev/go/ast) code rewriting to rewrite code for using [Uber FX](https://github.com/uber-go/fx).

## Behavior

Walk through the code and for constructors named `NewX()` returning `X`:

Rewrite 

1. Constructors are named as `NewX`
