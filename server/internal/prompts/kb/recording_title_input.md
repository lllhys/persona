{{if .Meeting}}一场会议{{else}}一段笔记{{end}}，{{.Start.Format "2006-01-02 15:04"}} 开始，已录约 {{.Minutes}} 分钟{{with .Language}}，语言 {{.}}{{end}}
{{with .Current}}现在的标题：「{{.}}」
{{end}}
转写：
{{.Transcript}}
