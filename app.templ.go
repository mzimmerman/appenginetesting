package appenginetesting

import (
	"text/template"
)

const appYAMLTemplString = `
application: {{.}}
version: 1
runtime: go
api_version: go1
module: appenginetestingfake

handlers:
- url: /.*
  script: _go_app
`

var appTempl = template.Must(template.New("app.yaml").Parse(appYAMLTemplString))
