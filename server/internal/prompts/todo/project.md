<persona-context kind="context" title="项目 · {{attr .Name}} · 背景" url="">
这件事属于这个项目（按你的工作目录找到的）。先看背景；下面的结论是之前同项目的任务做完时记下的，接着做的时候照着来，别再踩同样的坑。
{{with .OneLine}}
项目：{{.}}
{{end}}{{if .Outcomes}}
最近几件做完的事（新的在前）：
{{range .Outcomes}}- {{.When}} 「{{.Title}}」{{.Text}}
{{end}}{{end}}{{range .Extensions}}
扩展「{{ctx .Name}}」为这个项目整理的背景：
{{ctx .Text}}
{{end}}</persona-context>
