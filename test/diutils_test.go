package test

import (
	"testing"

	"github.com/debedb/fxforce5/diutils"
	"go.uber.org/fx"
)

type Foo struct {
	Name string
}

type FooParams struct {
	fx.In

	Name string
}

func NewFoo(p FooParams) *Foo {
	return diutils.Construct[FooParams, Foo](p)
}

func TestPtrRetval(t *testing.T) {
	fp := FooParams{Name: "foo"}
	f := NewFoo(fp)
	if f.Name != "foo" {
		t.Errorf("Expected foo, got %s", f.Name)
	}
}

type Bar struct {
	Name string
}

type BarParams struct {
	fx.In

	Name string
}

func NewBar(p BarParams) Bar {
	return diutils.ConstructVal[BarParams, Bar](p)
}

func TestNoPtrRetval(t *testing.T) {
	fp := FooParams{Name: "foo"}
	f := NewFoo(fp)
	if f.Name != "foo" {
		t.Errorf("Expected foo, got %s", f.Name)
	}
}
