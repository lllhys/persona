Persona 算好的事实（这次开跑时的快照，数字以此为准，别改也别另编）：
{{if .Card}}这些会由 Persona 原样列在发给用户的飞书卡片里（含链接和「通过」按钮），你不用再逐条列。{{else}}这次不会发飞书卡片（网页版里有同样的条目，手机 App 里没有）：请在一两句总述之后，按下面的顺序把它们每件一行列出来，说清要用户做什么、去哪点。{{end}}
{{if .WaitingUnknown}}- 等用户处理的：刚重启，还没读到，不要说「没有」。{{else}}- 等用户处理的 {{.WaitingTotal}} 件{{range .Waiting}}
  - {{.}}{{end}}{{end}}
- 过去 {{.Hours}} 小时没成 {{.FailedTotal}} 件{{range .Failed}}
  - {{.Title}}（{{if .Skipped}}被跳过{{else if .Run}}运行失败{{else}}没开起来{{end}}{{if .Why}}：{{.Why}}{{end}}）{{end}}
- 过去 {{.Hours}} 小时做完 {{.DoneTotal}} 件{{range .Done}}
  - {{.}}{{end}}
- 等用户验收的 {{.AcceptTotal}} 件{{range .Accept}}
  - {{.Line}}{{if .Changed}}（改了：{{.Changed}}）{{end}}{{end}}
{{if .LaterTotal}}- 可以晚点答的问题 {{.LaterTotal}} 件：Agent 留下的、不急的问题，列在早晚报的「可以晚点答」一节里。只说有几件，不要逐条再列，也不要替用户回答。
{{end}}- 正在做的 {{.ProgressTotal}} 件{{range .Progress}}
  - {{.Title}}{{if .Stage}}（阶段：{{.Stage}}）{{end}}{{end}}{{with .Leaders}}
- 各方向（每个主管一行；有工作记账的进度卡工具就发卡，没有才在早晚报里每个方向照着各写一行，卡住的写明卡在哪、谁来解）：{{range .}}
  - {{.Name}}：在办 {{.Open}} 件{{if .Waiting}}，等用户拍板 {{.Waiting}} 件{{end}}{{if .Stuck}}，卡住超过一天 {{.Stuck}} 件{{end}}{{if or .Reached .Doing .NeedsYou}}；主管今天的小结：{{with .Reached}}达成 {{.}}；{{end}}{{with .Doing}}在做 {{.}}；{{end}}{{with .NeedsYou}}要用户做 {{.}}{{end}}{{else}}；主管今天还没写小结{{end}}{{end}}{{end}}
