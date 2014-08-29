package appenginetesting

import (
	"text/template"
)

const queueTemplString = `total_storage_limit: 120M
queue:{{range .}}
- name: {{.}}
  rate: 35/s{{end}}
`

var queueTempl = template.Must(template.New("queue.yaml").Parse(queueTemplString))
