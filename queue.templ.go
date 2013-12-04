package appenginetesting

import (
	"text/template"
)

const queueTemplString = `
total_storage_limit: 120M
queue:{{range .}}
- name: {{.}}
  rate: 35/s{{end}}
`

var queueTempl = template.Must(template.New("queue.yaml").Parse(queueTemplString))

const appYAMLTemplString = `
application: {{.}}
version: 1
runtime: go
api_version: go1

handlers:
- url: /.*
  script: _go_app
`

var appTempl = template.Must(template.New("app.yaml").Parse(appYAMLTemplString))
