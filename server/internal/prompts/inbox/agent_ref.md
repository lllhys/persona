{{if .Lark}}飞书会话 id：{{.Lark.FeedID}}{{if .Lark.Account}}，在飞书账号 {{.Lark.Account}}{{end}}
{{else if .Doc}}文档链接：{{.Doc.URL}}
{{if .Doc.Rows}}如果某一行的状态该变（比如事情已经做完、开始跟进），在 inbox_suggest（kind reply）的 changes 里给出 [{ref, field, value}]（上面列出的 ref、field 和可选值之一），用户点发送时一起改；不该改就不给。{{end}}回复会作为评论发在文档里。
{{else if .Moment}}公司圈帖子 id：{{.Moment.PostID}}。回复会作为评论发在这个帖子下（回复上面标了「要回复的这条」的那条评论），全公司可见：只说能公开说的。
{{else if .Connector}}{{with .Connector}}{{if .Via}}这条来自订阅「{{.Name}}」：读的是连接器 {{.Via}}{{with .ViaSource}} 的「{{.}}」{{end}}，要看更早的上下文用这个连接器的读工具。
{{end}}{{if or (eq .Source "github") (eq .Via "github")}}GitHub 链接：{{.URL}}（github_read 读全文和评论，github_diff 看改动，github_checks 看 CI）
{{else if .URL}}链接：{{.URL}}
{{end}}{{with .ReplyTo}}回复会{{.}}。
{{end}}{{end}}{{else if .Mail}}邮箱账号 {{.Mail.Account}}，文件夹 {{.Mail.Mailbox}}，最新一封 uid {{.Mail.UID}}（mail_* 工具用）
{{end}}
