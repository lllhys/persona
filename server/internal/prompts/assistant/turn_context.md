<persona-context>
{{if .Full}}实时快照（这条消息发出时的；之后的变化用工具查）：{{else}}实时快照里和上一份比变了的（没列出的照旧）：{{end}}
{{range .Sections}}{{if .Partial}}{{if .Text}}{{ctx .Text}}
  - （只列有变化和新增的行，没列的行照旧）{{else}}{{ctx .Name}}（只列有变化和新增的行，没列的行照旧）：{{end}}{{range .Rows}}
{{ctx .}}{{end}}{{with .Removed}}
  - （已移除的行：{{ctx (join . "；")}}）{{end}}{{with .Order}}
  - （行现在的顺序：{{ctx (join . "；")}}）{{end}}{{else}}{{ctx .Text}}{{end}}
{{end}}{{with .Gone}}- 这份快照里已经没有的段落（它们之前的内容作废）：{{range .}}
  - {{ctx .}}{{end}}
{{end}}</persona-context>
