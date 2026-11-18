The conversation so far, oldest first:
{{range .}}
{{if .Assistant}}Assistant{{else}}User{{end}}:
{{.Text}}
{{end}}
Reply to the last message as the assistant: your reply only, without a speaker label.
