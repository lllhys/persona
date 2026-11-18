<persona-context kind="context" title="待办 · {{attr .Title}} · 已定的决策" url="">
这件事上用户已经拍过板的决定（只列现在生效的，按时间先后）。照这些做，不要再问同样的问题；要改其中哪一条，先问用户。
{{range .Decisions}}
{{.N}}. {{.Question}}
   选了：{{.Choice}}{{if .Reason}}
   原因：{{.Reason}}{{end}}
   （{{.When}}{{if eq .Source "outcome"}}，Agent 做完时记的，不是用户定的{{else if eq .Source "earlier-outcome"}}，前一阶段的记录，不代表待办当前已完成，也不是用户定的{{else if eq .Source "agent"}}，Agent 干活时记的，不是用户定的{{else if eq .Source "thread"}}，用户在飞书话题里回的{{else if eq .Source "card"}}，{{if .Asker}}在「{{.Asker}}」{{else}}在 Agent {{end}}的提问里答的{{end}}{{if .Ancestor}}{{if .Source}}；{{else}}，{{end}}前序任务「{{.Ancestor}}」{{end}}）
{{end}}</persona-context>
