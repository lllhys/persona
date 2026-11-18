{{if .Language}}<persona-wake>{{.Marker}}</persona-wake>
{{if eq .Language "日本語"}}⏰（Personaからの再開確認、第{{.Attempt}}回）前のターンで「{{.Wait}}」を待つと伝えました{{with .Then}}。完了後は「{{.}}」を行う予定です{{end}}。{{with .Agent}}
待っていたAgent「{{.}}」が停止しました{{with $.AgentLine}}。最後の報告：{{.}}{{end}}。{{end}}
待っていたことが完了したか自分で確認してください。完了していれば作業を続け、まだなら残っていることと必要な時間を簡潔に伝えてください（Personaが再度確認します）。待機し続ける必要はありません。
{{- if .Approved}}
{{if .ApprovedByTask}}この手順はタスクに含まれており、Personaはそのタスクの権限に基づいて許可しました{{else}}ユーザーはすでに許可しています{{end}}：待ちが解消したら「{{.Approved}}」を行ってください。この手順の再確認は不要です。それ以外に、コードのpush、デプロイ・公開、他者へのメッセージやコメント、他者のMR・PRのマージや変更を行う場合は、まず停止してユーザーに確認し、独断で実行しないでください。
{{- else}}
この後、コードのpush、デプロイ・公開、他者へのメッセージやコメント、他者のMR・PRのマージや変更を行う場合は、まず停止してユーザーに確認し、独断で実行しないでください。
{{- end}}
{{else}}⏰ (Persona is waking you, attempt {{.Attempt}}.) At the end of your last turn, you said you were waiting for “{{.Wait}}”{{with .Then}} and would then “{{.}}”{{end}}.{{with .Agent}}
The Agent you were waiting for, “{{.}}”, has stopped{{with $.AgentLine}}. Its last report: {{.}}{{end}}.{{end}}
Check whether what you were waiting for is ready. If it is, continue the work. Otherwise, briefly explain what remains and roughly how long it will take (Persona will wake you again). Do not keep waiting in a loop.
{{- if .Approved}}
{{if .ApprovedByTask}}Your task already authorized this step, and Persona allowed it under that authorization{{else}}The user has already approved this step{{end}}: when the wait is over, “{{.Approved}}”. Do not ask again for that step. For anything else that pushes code, deploys or publishes, sends messages or comments to others, or merges or changes someone else's MR or PR, stop and ask the user first; do not act on your own.
{{- else}}
If the next step pushes code, deploys or publishes, sends messages or comments to others, or merges or changes someone else's MR or PR, stop and ask the user first; do not act on your own.
{{- end}}
{{end}}{{else}}⏰（Persona 叫醒你，第 {{.Attempt}} 次）你上一轮停下时说在等「{{.Wait}}」{{with .Then}}，等到以后要「{{.}}」{{end}}。{{with .Agent}}
你等的 Agent「{{.}}」刚停下了{{with $.AgentLine}}，它最后说：{{.}}{{end}}。{{end}}
现在自己查一下等的事好了没有：好了就接着往下做；还没好就简短说明还差什么、大概还要多久（Persona 会再叫你），不用一直守着等。
{{- if .Approved}}
{{if .ApprovedByTask}}你的任务里已经交代要做这一步，Persona 已按任务授权放行{{else}}用户已经同意{{end}}：等好了就去「{{.Approved}}」，这一步不用再问。除此以外，接下来如果还要推代码、部署 / 发布、给别人发消息或评论、合并或改别人的 MR / PR，先停下来问用户，不要自己做。
{{- else}}
接下来如果要推代码、部署 / 发布、给别人发消息或评论、合并或改别人的 MR / PR，先停下来问用户，不要自己做。
{{- end}}{{end}}
