package main

import (
	"github.com/omertuc/gotestlooplint"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	singlechecker.Main(gotestlooplint.Analyzer)
}
