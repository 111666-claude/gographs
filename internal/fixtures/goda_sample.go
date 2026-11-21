package fixtures

import _ "embed"

// GodaSample is DOT captured from a real `goda graph -short -cluster ./...`
// run against this repository. It guards the theme rewriter against upstream
// changes in goda's DOT shape.
//
//go:embed goda_sample.dot
var GodaSample string
