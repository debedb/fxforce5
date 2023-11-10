# FxForce5

Attempt to use [AST-based](https://pkg.go.dev/go/ast) code rewriting to rewrite code for using [Uber FX](https://github.com/uber-go/fx).

## Behavior

Walk through the code and for constructors named `NewX()` returning `X`:

Rewrite 

1. Constructors are named as `NewX`

For why redundancy see https://github.com/uber-go/fx/discussions/1110

## Known issues

 * Free-floating comments are messed up (See https://github.com/golang/go/issues/20744). A solution could be using https://github.com/dave/dst.

## See also

 * https://github.com/uber-go/fx/discussions/1110

