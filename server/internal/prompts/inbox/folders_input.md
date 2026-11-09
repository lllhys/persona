{{with .Folders}}已有的文件夹（后面是里面的事项数）：
{{range .}}- {{.Path}}（{{.Count}}）
{{end}}
{{else}}还没有文件夹。

{{end}}{{with .Examples}}用户自己归档过的（照着用户的习惯来）：
{{range .}}- {{.Title}}{{with .Sender}}（{{.}}）{{end}} → {{.Folder}}
{{end}}
{{end}}要归档的事项：
{{range .Items}}
id：{{.ID}}
类型：{{.Kind}}{{with .Source}}（{{.}}）{{end}}
标题：{{.Title}}{{with .Sender}}
来自：{{.}}{{end}}{{with .Summary}}
摘要：{{.}}{{end}}{{with .Preview}}
内容：{{.}}{{end}}
{{end}}
