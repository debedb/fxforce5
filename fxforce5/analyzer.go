// Based on Pani Networks
// https://github.com/romana/core/tree/42682d9f8d6151ed7175528dbc4194e77744716f/tools
//
// http://www.apache.org/licenses/LICENSE-2.0

package fxforce5

import (
	"errors"
	"fmt"
	"go/printer"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"

	// TODO make even this dynamic
	"go/ast"
	"go/build"
	"go/doc"
	"go/parser"
	"go/token"

	// "github.com/romana/core/common"

	//	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

// Analyzer uses various reflection/introspection/code analysis methods to analyze
// code and store metadata about it, for various purposes. It works on a level of
// a Go "repository" (see https://golang.org/doc/code.html#Organization).
type Analyzer struct {
	// Path started with.
	path string

	// In Go convention, this is "src" directory under path (above). Saved
	// here to avoid doing path + "/src" all the time.s
	srcDir string

	// Module path parsed from go.mod file.
	modPath string

	// List of paths that have already been analyzed
	analyzed []string
	// All the import paths that we have gone through.
	importPaths []string

	buildPackages []build.Package
	astPackages   []ast.Package
	docPackages   []doc.Package
	astFiles      []*ast.File
	conf          *packages.Config
	objects       []types.Object
	fullTypeDocs  map[string]string
	shortTypeDocs map[string]string
	fileSet       *token.FileSet
}

// NewAnalyzer creates a new Analyzer object for analysis of Go project
// in the provided path.
func NewAnalyzer(path string) *Analyzer {
	// Hm do we need conf?
	conf := packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedDeps | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedModule,
		Dir: path}
	a := &Analyzer{
		path:     path,
		srcDir:   path + "/src",
		analyzed: make([]string, 0),
		conf:     &conf,
		// fullTypeDocs:  common.MkMapStr(),
		// shortTypeDocs: common.MkMapStr(),
		fileSet: token.NewFileSet(),
	}
	return a
}

var pathVariableRegexp regexp.Regexp

func (a *Analyzer) Analyze() error {
	f, err := os.Open(a.srcDir)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	err = f.Close()
	if !info.IsDir() {
		return errors.New(fmt.Sprintf("Expected %s to be a directory", a.srcDir))
	}

	goModFile := a.srcDir + "/go.mod"
	goModBuf, err := os.ReadFile(goModFile)
	if err != nil {
		return err
	}
	modFile, err := modfile.Parse(goModFile, goModBuf, nil)
	if err != nil {
		return err
	}
	a.modPath = modFile.Module.Mod.Path

	err = filepath.Walk(a.srcDir, a.walker)
	if err != nil {
		return err
	}
	log.Printf("Visited:\n%s", a.analyzed)

	// lprog, err := a.conf.Load()
	// if lprog == nil {
	// 	return err
	// }
	// log.Printf("Loaded program: %v, Error: %T %v", lprog, err, err)

	// for _, pkg := range lprog.InitialPackages() {
	// 	for k, v := range pkg.Types {
	// 		log.Printf("%v ==> %+v", k, v)
	// 	}

	// 	scope := pkg.Pkg.Scope()
	// 	for _, n := range scope.Names() {
	// 		obj := scope.Lookup(n)
	// 		log.Printf("Type: Type: %s: %s ", obj.Type().String(), obj.Id())
	// 		a.objects = append(a.objects, obj)
	// 	}
	// }

	// ssaProg := ssautil.CreateProgram(lprog, ssa.BuilderMode(ssa.GlobalDebug))
	// ssaProg.Build()

	// for _, p := range a.docPackages {
	// 	for _, t := range p.Types {
	// 		log.Printf("\n****\n%+v\n****\n", t)
	// 	}
	// }
	return nil
}

func (a *Analyzer) walker(path string, info os.FileInfo, err error) error {
	if path == a.srcDir {
		return nil
	}
	name := info.Name()
	//	log.Printf("Entered walker(\"%s\", \"%s\", %+v)", path, name, err)
	firstChar := string(name[0])
	isDotFile := firstChar == "."
	//	log.Printf("Checking %v vs .: %v", firstChar, isDotFile)
	if isDotFile {
		log.Printf("Ignoring (dotfile): %s", name)
		if info.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}

	if In(path, a.analyzed) {
		log.Printf("Ignoring (visited): %s in %+v", path, a.analyzed)
		return nil
	}
	a.analyzed = append(a.analyzed, path)
	if err != nil {
		log.Printf("Error in walking %s: %s", path, err)
		return err
	}
	if name == "vendor" {
		log.Printf("Ignoring (vendor): %s", path)
		return filepath.SkipDir
	}

	if strings.HasSuffix(name, ".go") {
		a.analyzeFile(path)
	}

	// if info.IsDir() {
	// 	err = a.analyzePath(path)
	// 	if err != nil {
	// 		log.Printf("Error in analyzePath(%s): %s", path, err)
	// 		return filepath.SkipDir
	// 	}
	// }

	return nil
}

type analyzedFile struct {
	path         string
	topNode      *ast.File
	structTypes  []*ast.TypeSpec
	constructors []*ast.FuncDecl
	imports      []*ast.ImportSpec
}

func (af *analyzedFile) applyPost(c *astutil.Cursor) bool {
	return true
}

func (af *analyzedFile) applyPre(c *astutil.Cursor) bool {
	n := c.Node()
	switch nType := n.(type) {
	case *ast.ImportSpec:
		af.imports = append(af.imports, nType)
	case *ast.TypeSpec:
		if nType.Type.(*ast.StructType) != nil {
			log.Printf("Found struct: %+v", nType.Name.Name)
			af.structTypes = append(af.structTypes, nType)
		}

	// case *ast.StructType:
	// 	log.Printf("Found struct: %+v", nType.Struct
	case *ast.FuncDecl:
		if strings.HasPrefix(nType.Name.Name, "New") {
			log.Printf("Found constructor: %+v", nType.Name.Name)
			af.constructors = append(af.constructors, nType)
			for i, param := range nType.Type.Params.List {
				log.Printf("\tParam %d: %+v", i, param)
			}
		}
	}
	return true
}

const UBER_FX_IMPORT = "\"go.uber.org/fx\""

func (af *analyzedFile) postProcess() {
	// 1. Add imports
	fxFound := false
	for _, imp := range af.imports {
		if imp.Path.Value == UBER_FX_IMPORT {
			fxFound = true
			break
		}
	}
	if !fxFound {
		af.topNode.Imports = append(af.topNode.Imports, &ast.ImportSpec{Path: &ast.BasicLit{Value: UBER_FX_IMPORT}})
	}

	// 2. Add module decl, like so:

	// 	var DependenciesModule = fx.Module("apiDependencies",
	// 	fx.Provide(NewRoutes),
	// 	fx.Provide(handlers.NewFileHandler),
	// )

	fxModuleArgs := []ast.Expr{&ast.BasicLit{Value: "TODOModule"}}

	for _, constructor := range af.constructors {
		providerCall := &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "fx"},
				Sel: &ast.Ident{Name: "Provide"}},
			Args: []ast.Expr{&ast.Ident{Name: constructor.Name.Name}}}
		fxModuleArgs = append(fxModuleArgs, providerCall)
	}

	fxModuleCall := &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: &ast.Ident{Name: "fx"}, Sel: &ast.Ident{Name: "Module"}},
		Args: fxModuleArgs}

	af.topNode.Decls = append(af.topNode.Decls, &ast.GenDecl{Tok: token.VAR,
		Specs: []ast.Spec{&ast.ValueSpec{
			Names:  []*ast.Ident{&ast.Ident{Name: "TODOModule"}},
			Values: []ast.Expr{fxModuleCall}}}})

	// 3. Rewrite constructors
}

func (af *analyzedFile) write() error {
	fset := token.NewFileSet()
	newPath := strings.Split(af.path, ".")[0] + "_new.go"
	fset.AddFile(newPath, fset.Base(), 0)

	outFile, err := os.Open(newPath)
	if err != nil {
		return err
	}
	err = printer.Fprint(outFile, fset, af.topNode)
	if err != nil {
		return err
	}
	fmt.Printf("Wrote %s\n", newPath)
	return nil
}

func (a *Analyzer) analyzeFile(path string) error {
	fset := token.NewFileSet()
	fmt.Printf("Analyzing %s\n", path)
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return err
	}
	af := &analyzedFile{path: path, topNode: node}
	// ast.Inspect(node, af.inspect)
	astutil.Apply(node, af.applyPre, af.applyPost)
	af.postProcess()
	err = af.write()
	return err
}

func (a *Analyzer) analyzePath(path string) error {

	importPath := path[len(a.path+"/src/"):]
	if importPath == "" {
		return nil
	}
	importPath = a.modPath + "/" + importPath
	a.importPaths = append(a.importPaths, importPath)

	// bpkg -- build packages
	bpkg, err := build.Import(importPath, "", 0)
	// If no Go files are found, we can skip it - not a real error.
	_, nogo := err.(*build.NoGoError)
	if err != nil {
		if !nogo {
			log.Printf("Error in build.Import(\"%s\"): %+v %T", path, err, err)
			return err
		}
		log.Printf("%s", err)
		return nil
	}
	//log.Printf("build.Import(%s, \"\", 0) = %+v", path, bpkg)

	files := make(map[string]*ast.File)
	goFiles := bpkg.GoFiles
	cGoFiles := bpkg.CgoFiles
	for _, name := range append(goFiles, cGoFiles...) {
		goFileName := filepath.Join(bpkg.Dir, name)
		// log.Printf("Processing %s...", goFileName)
		file, err := parser.ParseFile(a.fileSet, goFileName, nil, parser.ParseComments)
		a.astFiles = append(a.astFiles, file)
		// TODO do we need to do anything with file.Scope?
		//		log.Printf("Processed %s: (%+v, %s)", goFileName, file, err)
		if err != nil {
			return err
		}
		files[name] = file
	}

	//	typeConfig := &types.Config{}
	//	info := &types.Info{}
	//	tpkg, err := typeConfig.Check(importPath, a.fileSet, a.astFiles, info)
	//	log.Printf("Types package info: %+v", info)

	// apkg - ast packages
	apkg := &ast.Package{Name: bpkg.Name, Files: files}
	// dpkg - doc packages
	dpkg := doc.New(apkg, bpkg.ImportPath, doc.AllDecls|doc.AllMethods)

	log.Printf("In AST package %s, Doc package %s, Build package %s", apkg.Name, dpkg.Name, bpkg.Name)

	for _, t := range dpkg.Types {
		// fullName is full import path (github.com/romana/core/tenant) DOT name of type
		fullName := fmt.Sprintf("%s.%s", importPath, t.Name)
		a.fullTypeDocs[fullName] = t.Doc
		// shortName is just package.type
		shortName := fmt.Sprintf("%s.%s", dpkg.Name, t.Name)
		a.shortTypeDocs[shortName] = t.Doc
		log.Printf("\tType docs for %s (%s): %+v,", fullName, shortName, t.Doc)
		for _, m := range t.Methods {
			methodFullName := fmt.Sprintf("%s.%s", fullName, m.Name)
			a.fullTypeDocs[methodFullName] = m.Doc
			methodShortName := fmt.Sprintf("%s.%s", shortName, m.Name)
			a.shortTypeDocs[methodShortName] = m.Doc
			log.Printf("\tMethod docs for %s (%s): %+v", methodFullName, methodShortName, m.Doc)
		}
	}

	if apkg.Scope != nil && apkg.Scope.Objects != nil {
		for name, astObj := range apkg.Scope.Objects {
			log.Printf("AST Object %s.%s: %v", apkg.Name, name, astObj.Data)
		}
	}

	//	a.buildPackages = append(a.buildPackages, *bpkg)
	//	a.astPackages = append(a.astPackages, *apkg)
	a.docPackages = append(a.docPackages, *dpkg)
	//	log.Printf("Parsed %s:\nbuildPackage:\n\t%s\nastPackage\n\t%s\ndocPackage:\n\n%s", path, bpkg.Name, apkg.Name, dpkg.Name)

	// a.conf.Import(importPath)
	return nil
}

func (a *Analyzer) FindImplementors(interfaceName string) []types.Type {
	ifc := a.getInterface(interfaceName)
	implementors := a.getImplementors(ifc)
	return implementors
}

func (a *Analyzer) getImplementors(ifc *types.Interface) []types.Type {
	retval := make([]types.Type, 0)
	for _, o := range a.objects {
		log.Printf("\t\tChecking if %s implements %s", o.Type(), ifc)
		fnc, wrongType := types.MissingMethod(o.Type(), ifc, true)
		if fnc == nil {
			retval = append(retval, o.Type())
			continue
		} else {
			log.Printf("%T (%v) does not implement %s: missing %s, wrong type: %t", o, o, ifc, fnc, wrongType)
		}
	}
	return retval
}

func (a *Analyzer) getInterface(name string) *types.Interface {
	for _, o := range a.objects {
		if o.Type().String() == name {
			return o.Type().Underlying().(*types.Interface)
		}
	}
	return nil
}
