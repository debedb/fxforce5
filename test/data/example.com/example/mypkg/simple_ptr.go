package mypkg

type Foo struct {
	Name string
}

func NewFoo() *Foo {
	return &Foo{Name: "foo"}
}
