package main

import (
	"flag"
	"log"
	"os"

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
	log.SetOutput(os.Stderr)
	log.SetOutput(os.Stdout)
	flag.Parse()

	if len(flag.Args()) != 1 {
		log.Fatal("For usage: fxforce5 -h")
	}
	srcRoot := flag.Args()[0]
	log.Printf("Analyzing %s", srcRoot)
	// TODO add as a pattern, to skip things like /generated/**.go
	// ignores := []string{"internal/dependencies.go"}
	analyzer := fxforce5.NewAnalyzer(srcRoot, nil)
	analyzer.Analyze()

}
