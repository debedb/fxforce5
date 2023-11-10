package fxforce5

import (
	"errors"
	"fmt"
	"go/types"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/dave/dst/dstutil"
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
	// TODO should be configurable on CLI -- but the import from
	// this package would require modifying go.mod of the target.
	DIUTILS_LOCAL = true

	// Processed directive to ignore a file
	PROCESSED_DIRECTIVE = "// +fxforce5:processed"
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

	if name == "vendor" || name == "generated" {
		log.Printf("Ignoring path %s", path)
		return filepath.SkipDir
	}

	if strings.HasSuffix(name, ".go") {
		err = a.analyzeFile(path)
		if err != nil {
			log.Printf("Error in analyzing %s: %s", path, err)
		}
		// Really don't need this
		a.analyzed = append(a.analyzed, path)
	}

	return nil
}

type analyzedFile struct {
	path    string
	relPath string

	diutilsImportPath string

	topNode *ast.File

	topDstNode *dst.File

	// Constructed in inspect()
	structTypes []*ast.TypeSpec

	// Map of identifier of return type to constructor function.
	// Constructed in inspect()
	constructors map[string]*ast.FuncDecl

	// Map of identifier of struct types returned by constructors to
	// the declarations of corresponding fx params structs
	// Constructed in prepareParamStructs()
	paramStruct map[string]*ast.TypeSpec

	// If true, this file has already been processed.
	// This will be detected by the presence of PROCESSED_DIRECTIVE.
	// This is detected by applyPreDst() (not by inspect() which detects
	// everything in the first pass because for some reason the ast handling
	// of comments even on read is flaky. We want to detect this directive
	// in the top of the file.
	alreadyProcessed bool

	hasUberFxImport  bool
	hasDiUtilsImport bool

	existingModuleVar string

	// Because walker (apply{Pre,Post} or Inspect) functions cannot return an error
	// we'll store it here and return it after the walking.
	err error

	imports []*ast.ImportSpec
}

// Noop because we only use this in post-processing step anyway
// func (af *analyzedFile) applyPost(c *astutil.Cursor) bool {
// 	return true
// }

func (af *analyzedFile) applyPostDst(c *dstutil.Cursor) bool {
	n := c.Node()
	switch nType := n.(type) {

	case *dst.ImportSpec:
		if !af.hasUberFxImport {
			c.InsertBefore(&dst.ImportSpec{Path: &dst.BasicLit{Value: UBER_FX_IMPORT}})
			af.hasUberFxImport = true
		}
		if !af.hasDiUtilsImport {
			c.InsertBefore(&dst.ImportSpec{Path: &dst.BasicLit{
				Value: "\"" + af.diutilsImportPath + "\""}})
			af.hasDiUtilsImport = true
		}

	case *dst.File:
		// Detect if processed, if so, set the alreadyProcessed flag and return false
		beforeDecs := nType.Decs.Start.All()
		for _, s := range beforeDecs {
			if s == PROCESSED_DIRECTIVE {
				af.alreadyProcessed = true
				return false
			}
		}
		// Otherwise, set PROCESSED_DIRECTIVE for next time
		nType.Decs.Start.Prepend(PROCESSED_DIRECTIVE)

	case *dst.ValueSpec:
		fmt.Print("ValueSpec: ")

	case *dst.FuncDecl:
		// Replace constructor now
		if strings.HasPrefix(nType.Name.Name, "New") {
			ctorName := nType.Name.Name
			log.Printf("Found constructor: %+v", ctorName)

			// Rename original one

			nType.Name.Name = ctorName + "Orig"
			c.Replace(nType)

			// Only one result here; otherwise would be an error on preprocessing.
			results := nType.Type.Results.List
			result := results[0]

			// TODO we could have already saved it from the original parse
			// Plus we'll need to make distinction between pointer and non-pointer
			origStructName := result.Type.(*dst.Ident).Name
			paramStructName := origStructName + "Params"

			arg := &dst.Field{Names: []*dst.Ident{{Name: "params"}},
				Type: &dst.Ident{Name: paramStructName}}
			args := []*dst.Field{arg}

			// return diutils.Construct[ServerParams, ServerCfg](p)
			constructCall := &dst.CallExpr{}
			constructGenericParams := []dst.Expr{
				&dst.Ident{Name: origStructName},
				&dst.Ident{Name: paramStructName},
			}

			constructCall.Fun = &dst.IndexListExpr{
				// diutils.Construct
				X: &dst.SelectorExpr{
					X:   &dst.Ident{Name: "diutils"},
					Sel: &dst.Ident{Name: "Construct"},
				},
				// Generic type parameters
				Indices: constructGenericParams,
			}
			constructCall.Args = []dst.Expr{&dst.Ident{Name: "params"}}

			retStmt := &dst.ReturnStmt{Results: []dst.Expr{constructCall}}
			body := &dst.BlockStmt{
				List: []dst.Stmt{retStmt},
			}

			ctorResults := &dst.FieldList{
				List: []*dst.Field{
					{Type: &dst.Ident{Name: origStructName}},
				},
			}

			newCtor := &dst.FuncDecl{
				Name: &dst.Ident{Name: ctorName},
				Type: &dst.FuncType{
					Func:    true,
					Params:  &dst.FieldList{List: args},
					Results: ctorResults,
				},
				Body: body,
			}
			c.InsertBefore(newCtor)
		}

	case *dst.GenDecl:
		// Add fx.Module declaration if needed, after imports
		if nType.Tok == token.IMPORT {
			if af.existingModuleVar != "" {
				log.Printf("Skipping adding fx.Module declaration to %s -- already exists as %+v\n", af.relPath, af.existingModuleVar)
				break
			}
			decl := af.getFxModuleDecl()
			dstDecl, err := decorator.NewDecorator(nil).DecorateNode(decl)
			if err != nil {
				af.err = err
				return false
			}
			c.InsertAfter(dstDecl)
		}

	// Add params struct
	case *dst.TypeSpec:
		if nType.Type.(*dst.StructType) == nil {
			break
		}
		origStructName := nType.Name.Name
		paramStructDecl := af.paramStruct[origStructName]
		if paramStructDecl == nil {
			log.Printf("No param struct for %s\n", origStructName)
			break
		}
		log.Printf("Inserting %s after %s\n", paramStructDecl.Name.Name, nType.Name.Name)
		// TODO this would group all the type decls together. It can be done separately
		// similar to how it is done for the fx.Module declaration

		dstParamStructDecl, err := decorator.NewDecorator(nil).DecorateNode(paramStructDecl)
		if err != nil {
			af.err = err
			return false
		}
		c.InsertAfter(dstParamStructDecl)
	}
	return true
}

// This is used to better structure changes in the post-processing step
func (af *analyzedFile) applyPre(c *astutil.Cursor) bool {
	n := c.Node()
	switch nType := n.(type) {
	case *ast.GenDecl:

		// Add fx.Module declaration if needed, after imports
		if nType.Tok == token.IMPORT {
			if af.existingModuleVar != "" {
				log.Printf("Skipping adding fx.Module declaration to %s -- already exists as %+v\n", af.relPath, af.existingModuleVar)
				break
			}
			decl := af.getFxModuleDecl()
			c.InsertAfter(decl)
		}
	// Add params struct
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
		// TODO this would group all the type decls together. It can be done separately
		// similar to how it is done for the fx.Module declaration
		c.InsertAfter(paramStructDecl)
	}
	return true
}

// First pass -- analyze the file and collect information about it.
// No changes to the AST are made here.
// TODO make it a pass also using dst
func (af *analyzedFile) inspect(n ast.Node) bool {
	switch nType := n.(type) {

	case *ast.ImportSpec:
		if nType.Path.Value == UBER_FX_IMPORT {
			af.hasUberFxImport = true
		}
		if nType.Path.Value == af.diutilsImportPath {
			af.hasDiUtilsImport = true
		}

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
				errMsg := fmt.Sprintf("%s: Constructor %s has %d results, expected 1", af.relPath, nType.Name.Name, results.NumFields())
				log.Println(errMsg)
				af.err = errors.New(errMsg)
				return false
			}
			resType := results.List[0].Type
			log.Printf("Result type: %+v", resType)
			resTypeKey := *&resType.(*ast.Ident).Name
			if af.constructors == nil {
				af.constructors = make(map[string]*ast.FuncDecl)
			}
			if af.constructors[resTypeKey] != nil {
				errMsg := fmt.Sprintf("%s: Constructor for %s already exists: %+v", af.relPath, resTypeKey, af.constructors[resTypeKey])
				log.Println(errMsg)
				af.err = errors.New(errMsg)
				return false
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
		// This is a lot of nested ifs, sigh.
		for _, spec := range nType.Specs {
			if valSpec, ok := spec.(*ast.ValueSpec); ok {
				for valSpecIdx, expr := range valSpec.Values {
					if callExpr, ok := expr.(*ast.CallExpr); ok {
						if selExpr, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
							if xExpr, ok := selExpr.X.(*ast.Ident); ok {
								if xExpr.Name == "fx" && selExpr.Sel.Name == "Module" {
									af.existingModuleVar = valSpec.Names[valSpecIdx].Name
									return true
								}
							}
						}
					}
				}
			}
		}
	}
	return true
}

//	 Get the fx module declaration for this file. It will look like this:
//
//		var DependenciesModule = fx.Module("apiDependencies",
//		fx.Provide(NewRoutes),
//		fx.Provide(handlers.NewFileHandler),
//
//	 We'll call it in the Apply to add right after the imports clause
//
// )
func (af *analyzedFile) getFxModuleDecl() *ast.GenDecl {
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

	fxModuleVarDecl := &ast.GenDecl{
		Tok: token.VAR,
		Specs: []ast.Spec{
			&ast.ValueSpec{
				Names:  []*ast.Ident{{Name: fxModName}},
				Values: []ast.Expr{fxModuleCall},
			},
		},
	}
	return fxModuleVarDecl
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
		// TODO still use ast.TypeSpec because dst.TypeSpec has some issues,
		// so for now it's a mix of ast and dst
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

// Return true if there were any changes to the file.
func (af *analyzedFile) process() (bool, error) {
	if af.err != nil {
		return false, af.err
	}

	if af.constructors == nil || len(af.constructors) == 0 {
		log.Printf("Skipping post-processing for %s -- no constructors\n", af.relPath)
		return false, nil
	}

	// astutil.Apply(af.topNode, af.applyPre, af.applyPost)
	af.prepareParamStructs()
	dstutil.Apply(af.topDstNode, nil, af.applyPostDst)

	// This will only be detected in the post-processing step for now.
	// When we replace inspect with dstUtil apply as first pass, this is to be moved up.
	if af.alreadyProcessed {
		log.Printf("Skipping post-processing for %s -- already processed\n", af.relPath)
		return false, nil
	}

	if af.err != nil {
		return false, af.err
	}

	return true, nil
}

func (af *analyzedFile) write() error {
	fset := token.NewFileSet()
	newPath := strings.Split(af.path, ".")[0] + "_new.go"
	fset.AddFile(newPath, fset.Base(), 0)

	outFile, err := os.OpenFile(newPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	// err = printer.Fprint(outFile, fset, af.topNode)
	restorer := decorator.NewRestorer()
	err = restorer.FileRestorer().Fprint(outFile, af.topDstNode)

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

	dstNode, err := decorator.NewDecorator(nil).ParseFile(path, nil, parser.ParseComments)

	diutilsImportPath := DIUTILS_IMPORT
	if DIUTILS_LOCAL {
		diutilsImportPath = a.modPath + "/diutils"
	}
	af := &analyzedFile{
		path:              path,
		diutilsImportPath: diutilsImportPath,
		relPath:           path[len(a.path+"/src/"):],
		topNode:           node,
		topDstNode:        dstNode}

	// This is done in several passes. We use Inspect at the first pass because we cannot
	// do any changes until we collect all the information. Then we use Apply to do the
	// changes.
	ast.Inspect(node, af.inspect)
	processed, err := af.process()
	if err != nil {
		return err
	}
	if !processed {
		log.Printf("No changes for %s\n", path)
		return nil
	}
	err = af.write()
	return err
}
