This meeting, being recorded now (times are minutes:seconds from its start):
<this_meeting>
{{with .Title}}Title: {{attr .}}
{{end}}{{with .Calendar}}{{with .Title}}Calendar title: {{attr .}}
{{end}}{{with .Attendees}}Calendar attendees: {{range $i, $a := .}}{{if $i}}, {{end}}{{attr $a}}{{end}}
{{end}}{{with .Location}}Calendar location: {{attr .}}
{{end}}{{end}}{{with .Language}}Language: {{attr .}}
{{end}}Said so far (the latest lines, oldest first):
{{range .Lines}}[{{.At}}] {{attr .Text}}
{{else}}(nothing yet)
{{end}}{{with .Asks}}Already answered in this meeting:
{{range .}}- Q: {{attr .Question}}
  A: {{attr .Answer}}
{{end}}{{end}}</this_meeting>

Earlier meetings, by their minutes:
{{range .Earlier}}<earlier_meeting id="{{attr .ID}}">
Title: {{attr .Title}}
{{with .When}}When: {{attr .}}
{{end}}{{with .Attendees}}Attendees: {{range $i, $a := .}}{{if $i}}, {{end}}{{attr $a}}{{end}}
{{end}}{{with .Points}}Points:
{{range .}}- {{attr .Text}}{{with .Who}} ({{attr .}}){{end}}
{{end}}{{end}}{{with .Decisions}}Decisions:
{{range .}}- {{attr .}}
{{end}}{{end}}{{with .Actions}}Action items:
{{range .}}- {{with .Who}}{{attr .}}: {{end}}{{attr .What}}{{with .By}} (by {{attr .}}){{end}}{{with .DueDate}} (due {{.}}){{end}}{{if eq .Todo "suggestion"}} [suggested, not made a to-do]{{else if .Todo}} [to-do {{.Todo}}]{{end}}
{{end}}{{end}}{{with .Open}}Open questions:
{{range .}}- {{attr .}}
{{end}}{{end}}{{with .Projects}}Projects:
{{range .}}- <project slug="{{attr .Slug}}">{{attr .Name}}</project> ({{.Status}})
{{end}}{{end}}</earlier_meeting>
{{else}}(none with minutes)
{{end}}
{{if .Question}}The question, typed on the phone:
<question>{{attr .Question}}</question>{{else}}No question was typed: the user tapped at [{{.HeardAt}}]. The lines said in the 45 seconds before the tap:
<heard>
{{range .Heard}}[{{.At}}] {{attr .Text}}
{{end}}</heard>{{end}}
