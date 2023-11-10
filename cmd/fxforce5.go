package main

import (
	"flag"
	"log"

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

	flag.Parse()

	if len(flag.Args()) != 1 {
		log.Fatal("For usage: fxforce5 -h")
	}
	srcRoot := flag.Args()[0]
	// TODO add as a pattern, to skip things like /generated/**.go
	// ignores := []string{"internal/dependencies.go"}
	analyzer := fxforce5.NewAnalyzer(srcRoot, nil)
	analyzer.Analyze()

}
