# FxForce5

Attempt to use [AST-based](https://pkg.go.dev/go/ast) code rewriting to rewrite code for using [Uber FX](https://github.com/uber-go/fx). Starting out with a particular use case. 

## Behavior

This expects a module that has:

1. Some `struct` such as

```
type X struct {
 Field1 Type1
 Field2 Type2
 ...
}
```

2. And a constructor `NewX` such as:

```
func NewX(Field1 Type1, Field2 Type2, ...) X
```

or


```
func NewX(Field1 Type1, Field2 Type2, ...) *X
```


(The return type can either be a value `X` or pointer to it `*X`).

The behavior is to walk through the code and rewrite it as follows:

1. Add an `XParams struct`:
```
type X struct {
 fx.In
 
 Field1 Type1
 Field2 Type2
 ...
}
```

(For why the duplication of the fields is needed instead of just embedding `X` in `XParams`, see discussion at https://github.com/uber-go/fx/discussions/1110. 

2. Replace `NewX` constructor with `NewXOrig`.

3. Add a new constructor `NewX` such as:

```
func NewX(params XParams) X {
  diutils.ConstructVal[X, XParams](params)
}
```
or

```
func NewX(params XParams) *X {
  diutils.Construct[X, XParams](params)
}
```

The `diutils.Construct()` or `diutils.ConstructVal()` uses reflection to properly assign fields.

4. Add, if needed, appropriate imports: `go.uber.org/fx` and/or `github.com/debedb/fxforce5/diutils`.

5. Add, if needed, the above dependencies into `go.mod` (TODO).

6. Add an `fx.Module` var:

```
var X = fx.Module("X", fx.Provide(NewX))
```

## Known issues

## See also

 * https://github.com/uber-go/fx/discussions/1110

