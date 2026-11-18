{{- /* The first message of a meeting's room (docs/assistant/meeting-minutes.md "Its first message"), message source
meeting: the upload's title and calendar hint (never the knowledge base's matched calendar item), the date and start
time in the user's zone, the language and the role (notes, #8851: no questions come until it changes). Every text through ctx. */ -}}
The meeting is starting. Write nothing now.

<persona-context kind="context" title="{{attr .Title}}" url="">
Title: {{ctx .Title}}
{{with .Calendar}}{{with .Title}}Calendar: {{ctx .}}
{{end}}{{with .Location}}Where: {{ctx .}}
{{end}}{{with .Attendees}}Invited: {{range $i, $a := .}}{{if $i}}, {{end}}{{ctx $a}}{{end}}
{{end}}{{end}}Started: {{.Started}}
Language: {{.Language}}
Role: {{if eq .Role "participant"}}participant: answers may be said aloud{{else if eq .Role "notes"}}notes: you keep the topics and highlights only, and nobody asks you anything until the role changes{{else}}listener: answers are shown on screen only{{end}}
</persona-context>
