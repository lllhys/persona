第一句话：
{{.Sentence}}

已有的主管（id：名字）：{{range .Leaders}}
- {{.ID}}：{{.Title}}{{with .Project}}（项目 {{.}}）{{end}}{{else}}
- 没有{{end}}

知识库里的项目：{{range .Projects}}
- {{.}}{{else}}
- 没有{{end}}
