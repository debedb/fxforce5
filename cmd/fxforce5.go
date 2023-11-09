package main

import (
	"fmt"

	"github.com/debedb/fxforce5/fxforce5"
)

// func walker(path string, info os.FileInfo, err error) error {
// 	fmt.Println(path)

// 	if info.IsDir() {
// 		return nil
// 	}
// 	if !strings.HasSuffix(path, ".go") {
// 		return nil
// 	}

// 	return nil
// }

// TODO command line options
func main() {
	fmt.Println("Hello, World!")
	srcRoot := "/Users/gregory/g/git/catalog-be-go2"
	// ignores := []string{"internal/dependencies.go"}
	analyzer := fxforce5.NewAnalyzer(srcRoot, nil)
	analyzer.Analyze()

}
