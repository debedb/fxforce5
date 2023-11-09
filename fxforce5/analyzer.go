package fxforce5

import (
	"errors"
	"fmt"
	"go/printer"
	"go/types"
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

const (
	UBER_FX_IMPORT = "\"go.uber.org/fx\""
	DIUTILS_IMPORT = "\"github.com/debedb/fxforce5/diutils\""

	// Whether to import diutils module as local to the
	// analyzed project (true) or from DIUTILS_IMPORT (false)
	// TODO should be configurable on CLI
	DIUTILS_LOCAL = true
)

// Analyzer uses various reflection/introspection/code analysis methods to analyze
// code and store metadata about it, for various purposes. It works on a level of
// a Go "repository" (see https://golang.org/doc/code.html#Organization).
type Analyzer struct {
	// Path started with.
	path string

	// List of files to ignore.
	ignores []string

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
func NewAnalyzer(path string, ignores []string) *Analyzer {
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
		// Really don't need this
		a.analyzed = append(a.analyzed, path)
	}

	return nil
}

type analyzedFile struct {
	path    string
	relPath string

	diutilsImportPath string

	topNode     *ast.File
	structTypes []*ast.TypeSpec

	// Map of identifier of return type to constructor function
	constructors map[string]*ast.FuncDecl

	// Map of identifier of struct types returned by constructors to
	// the declarations of corresponding fx params structs
	paramStruct map[string]*ast.TypeSpec

	imports []*ast.ImportSpec
}

// Noop because we only use this in post-processing step anyway
func (af *analyzedFile) postProcessApplyPost(c *astutil.Cursor) bool {
	return true
}

// This is used to better structure changes in the post-processing step
func (af *analyzedFile) postProcessApplyPre(c *astutil.Cursor) bool {
	n := c.Node()
	switch nType := n.(type) {
	case *ast.TypeSpec:
		if nType.Type.(*ast.StructType) == nil {
			break
		}
		paramStructDecl := af.paramStruct[nType.Name.Name]
		if paramStructDecl == nil {
			log.Printf("No param struct for %s\n", nType.Name.Name)
			break
		}
		log.Printf("Inserting %s after %s\n", paramStructDecl.Name.Name, nType.Name.Name)

		c.InsertAfter(paramStructDecl)
	}
	return true
}

func (af *analyzedFile) inspect(n ast.Node) bool {
	switch nType := n.(type) {
	case *ast.ImportSpec:
		af.imports = append(af.imports, nType)

	case *ast.TypeSpec:
		if nType.Type.(*ast.StructType) != nil {
			log.Printf("Found struct: %+v", nType.Name.Name)
			af.structTypes = append(af.structTypes, nType)
		}

	case *ast.FuncDecl:
		if strings.HasPrefix(nType.Name.Name, "New") {
			log.Printf("Found constructor: %+v", nType.Name.Name)
			results := nType.Type.Results
			if results.NumFields() != 1 {
				log.Printf("Constructor %s has %d results, expected 1", nType.Name.Name, results.NumFields())
				return true
			}
			resType := results.List[0].Type
			log.Printf("Result type: %+v", resType)
			resTypeKey := *&resType.(*ast.Ident).Name
			if af.constructors == nil {
				af.constructors = make(map[string]*ast.FuncDecl)
			}
			if af.constructors[resTypeKey] != nil {
				log.Printf("Constructor for %s already exists: %+v\n", resType, af.constructors[resTypeKey])
				return true
			}
			af.constructors[resTypeKey] = nType
			for i, param := range nType.Type.Params.List {
				log.Printf("\tParam %d: %+v", i, param)
			}
		}

	case *ast.GenDecl:
		// Look for var declarations having fx.Module -- to skip calling
		// addFxModule if so
		if nType.Tok != token.VAR {
			return true
		}
		// TODO

	}
	return true
}

// 2. Add module decl, like so:
//
//	var DependenciesModule = fx.Module("apiDependencies",
//	fx.Provide(NewRoutes),
//	fx.Provide(handlers.NewFileHandler),
//
// )
func (af *analyzedFile) addFxModule() {
	// Figure out name of fx module
	fxModName := strings.Split(af.relPath, ".")[0]
	fxModParts := strings.Split(fxModName, "/")
	fxModName = ""
	for _, part := range fxModParts {
		fxModName += strings.ToUpper(part[0:1]) + part[1:]
	}
	fxModNameQuoted := "\"" + fxModName + "\""

	fxModuleArgs := []ast.Expr{&ast.BasicLit{Value: fxModNameQuoted}}

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

	fxModuleVarDecl := &ast.GenDecl{Tok: token.VAR,
		Specs: []ast.Spec{&ast.ValueSpec{
			Names:  []*ast.Ident{{Name: fxModName}},
			Values: []ast.Expr{fxModuleCall}}}}
	// Put it on top
	af.topNode.Decls = append([]ast.Decl{fxModuleVarDecl}, af.topNode.Decls...)
}

func (af *analyzedFile) addImports() {
	fxFound := false
	diutilsFound := false
	for _, imp := range af.imports {
		if imp.Path.Value == UBER_FX_IMPORT {
			fxFound = true
		}
		if imp.Path.Value == af.diutilsImportPath {
			diutilsFound = true
		}
		if fxFound && diutilsFound {
			break
		}
	}
	if !fxFound {
		af.topNode.Imports = append(af.topNode.Imports, &ast.ImportSpec{Path: &ast.BasicLit{Value: UBER_FX_IMPORT}})
	}
	if !diutilsFound {
		af.topNode.Imports = append(af.topNode.Imports,
			&ast.ImportSpec{Path: &ast.BasicLit{
				Value: "\"" + af.diutilsImportPath + "\""}})
	}
}

// Prepare param struct declarations for all structs that have constructors.
// This is done in a separate pass because we need to know all the constructors.
// We will then pass it to the astutil.Apply() function to do the actual
// insertion of those just so they can be inserted after the struct declaration
// Fields have to be copied from the original struct, but fx.In has to be added
// Unfortunately, merely embedding the original struct does not work.
// See also https://github.com/uber-go/fx/discussions/1110
func (af *analyzedFile) prepareParamStructs() {
	if af.paramStruct == nil {
		af.paramStruct = make(map[string]*ast.TypeSpec)
	}

	for _, structType := range af.structTypes {
		if af.constructors[structType.Name.Name] == nil {
			log.Printf("Ignoring struct %s as it does not have a constructor", structType.Name.Name)
			continue
		}

		paramStructFields := &ast.FieldList{
			List: make([]*ast.Field, 0),
		}

		// Add fx.In as first field
		paramStructFields.List = append(paramStructFields.List, &ast.Field{
			Type: &ast.Ident{Name: "fx.In"},
		})

		for _, field := range structType.Type.(*ast.StructType).Fields.List {
			paramStructFields.List = append(paramStructFields.List, field)
		}

		paramStruct := &ast.StructType{
			Fields: paramStructFields,
		}
		paramTypeSpec := &ast.TypeSpec{
			Name: &ast.Ident{Name: structType.Name.Name + "Params"},
			Type: paramStruct,
		}
		// paramTypeDecl := &ast.GenDecl{
		// 	Tok:   token.TYPE,
		// 	Specs: []ast.Spec{paramTypeSpec},
		// }

		af.paramStruct[structType.Name.Name] = paramTypeSpec

	}
}

func (af *analyzedFile) postProcess() {
	af.addImports()

	af.addFxModule()
	af.prepareParamStructs()

	astutil.Apply(af.topNode, af.postProcessApplyPre, af.postProcessApplyPost)
}

func (af *analyzedFile) write() error {
	fset := token.NewFileSet()
	newPath := strings.Split(af.path, ".")[0] + "_new.go"
	fset.AddFile(newPath, fset.Base(), 0)

	outFile, err := os.OpenFile(newPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	err = printer.Fprint(outFile, fset, af.topNode)
	if err != nil {
		return err
	}
	outFile.Close()
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
	diutilsImportPath := DIUTILS_IMPORT
	if DIUTILS_LOCAL {
		diutilsImportPath = a.modPath + "/diutils"
	}
	af := &analyzedFile{
		path:              path,
		diutilsImportPath: diutilsImportPath,
		relPath:           path[len(a.path+"/src/"):],
		topNode:           node}

	// This is done in several passes. We use Inspect at the first pass because we cannot
	// do any changes until we collect all the information. Then we use Apply to do the
	// changes.
	ast.Inspect(node, af.inspect)
	af.postProcess()
	err = af.write()
	return err
}
