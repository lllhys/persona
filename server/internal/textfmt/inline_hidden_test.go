package textfmt

import (
	"strings"
	"testing"
)

func TestSearchMarkdownHidesInlineScriptAndStyle(t *testing.T) {
	cases := []struct{ input, want string }{
		{`Visible <script>hidden_script_needle</script> answer`, "Visible answer"},
		{`Visible <STYLE media="screen">hidden_style_needle</STYLE> answer`, "Visible answer"},
		{`Visible <script class="x">hidden **formatted** and ` + "`code`" + ` <b>body</b></script> answer`, "Visible answer"},
		{`Visible <script/>hidden</script> answer`, "Visible answer"},
		{`Visible <script><!-- </script> --> answer`, "Visible --> answer"},
		{"Visible <script>hidden\n\nstill hidden\n\n</script> answer", "Visible\nanswer"},
		{"Visible <script>hidden\n\nlast hidden", "Visible"},
		{"Visible <script>hidden\n\n<div>\n</script>after\n</div>\n\nvisible", "Visible\nafter\nvisible"},
		{`Visible <script>hidden</style>still hidden</script> after`, "Visible after"},
		{`Visible <b title="<script>">safe</b> after`, "Visible safe after"},
		{`Visible <b>important</b> answer`, "Visible important answer"},
		{"Show `<script>example</script>` now", "Show <script>example</script> now"},
		{"```html\n<script>example</script>\n```", "<script>example</script>"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := SearchMarkdown(tc.input); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSearchMarkdownKeepsOrdinaryPreviewUnchanged(t *testing.T) {
	input := `Visible <script>script_text</script> after`
	if got := PlainMarkdown(input); !strings.Contains(got, "script_text") {
		t.Fatalf("ordinary preview behavior changed: %q", got)
	}
}
