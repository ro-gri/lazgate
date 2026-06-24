package main

import (
	"flag"
	"fmt"
	"os"

	"laz/internal/protocol/protogen"
)

func main() {
	specPath := flag.String("spec", "", "protocol agent JSON spec")
	outPath := flag.String("out", "", "output proto file")
	goOutPath := flag.String("go-out", "", "output Go manifest builder file")
	flag.Parse()

	if err := protogen.GenerateFiles(*specPath, *outPath, *goOutPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
