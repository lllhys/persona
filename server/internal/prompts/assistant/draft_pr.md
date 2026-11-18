{{if eq .Language "English"}}Our linked PR {{.URL}} at head {{.Head}} is still a draft, not waiting for upstream review or merge.
{{else if or (eq .Language "Japanese") (eq .Language "日本語")}}関連する自分の PR {{.URL}}（head {{.Head}}）はまだドラフトで、上流のレビューやマージ待ちではありません。
{{else}}关联的自家 PR {{.URL}}（head {{.Head}}）仍是草稿，不是在等上游评审或合并。
{{end}}
