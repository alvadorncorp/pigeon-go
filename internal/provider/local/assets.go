package local

import (
	_ "embed"
	"html/template"
)

//go:embed local.html
var localHTML string

//go:embed local.css
var localCSS string

//go:embed local.js
var localJS string

//go:embed local.svg
var localSVG string

var localTemplate = template.Must(template.New("inbox").Parse(localHTML))
