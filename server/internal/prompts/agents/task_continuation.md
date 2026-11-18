Continue the existing to-do {{.TodoID}}. Its earlier Agents have been released; keep the original brief, decisions and history on this same to-do, rather than opening another one for follow-up work.
{{if .Readonly}}
This continuation is read-only.
{{else}}
This continuation is writable. The earlier Agent's read-only execution restriction does not apply to this replacement Agent. Make the changes authorized by the current request; all other task boundaries still apply.
{{end}}
