{{- if eq .Lang "en" -}}
{{- $to := "the Persona main conversation"}}{{if .Agent}}{{$to = "the agent"}}{{end}}{{with .To}}{{$to = .}}{{end -}}
{{- if eq .Code "unprompted" -}}
"{{.Task}}" was not sent to {{$to}}: Persona did not hear a new request from the user since the last thing you were told about an agent's work (or they had not spoken in the last half minute), so it held it back to be safe. Tell the user in one sentence that you didn't hear a fresh request from them since the last result (or lately), so you held it back to be safe; if they want it done, they can say it again{{if .Resend}} or tap Send Anyway on the screen{{end}}. Do not make it sound as if someone else said it.
{{- else if eq .Code "empty" -}}
It was not clear what to hand to {{$to}}, so nothing was sent. Ask the user in one sentence to say it again.
{{- else if eq .Code "unknown" -}}
Not sure whether "{{.Task}}" reached {{$to}}: the connection broke off while sending. Do not hand it over again; ask the user in one sentence to look at it on the phone.
{{- else if eq .Code "unreachable" -}}
"{{.Task}}" was not sent to {{$to}}: Persona could not be reached. Tell the user in one sentence that it was not sent.
{{- else if eq .Code "on_screen" -}}
"{{.Task}}" was not sent to {{$to}}: it only says yes or no, and {{$to}} is waiting on a card (a plan, a permission). Cards are answered by tapping them on the screen, not by voice. Tell the user in one sentence to tap it on the screen.
{{- else if eq .Code "dialog_open" -}}
"{{.Task}}" was not sent to {{$to}}: {{if .Plan}}its plan is waiting on the screen. The user can tap there to send back what they just said as feedback on the plan{{else}}a dialog in its terminal is waiting to be dealt with first, on the screen{{end}}. Nothing was typed. Tell the user that in one sentence.
{{- else if eq .Code "input_draft" -}}
"{{.Task}}" was not sent to {{$to}}: its input box on the computer holds words typed there and not sent. Nothing was typed. Tell the user in one sentence; the screen offers to clear them and send.
{{- else if .Reason -}}
"{{.Task}}" was not sent to {{$to}}: {{.Reason}}
Tell the user in one sentence that it was not sent, and why.
{{- else -}}
"{{.Task}}" was not sent to {{$to}}. Tell the user in one sentence that it was not sent.
{{- end -}}
{{- else if eq .Lang "ja" -}}
{{- $to := "Persona のメイン会話"}}{{if .Agent}}{{$to = "エージェント"}}{{end}}{{with .To}}{{$to = print . " "}}{{end -}}
{{- if eq .Code "unprompted" -}}
「{{.Task}}」は{{$to}}に送っていません：エージェントの作業内容を最後に伝えてから、ユーザーの新しい依頼が聞こえなかった（または最後に話してから 30 秒以上たっている）ため、安全のため送らずにおきました。前回の結果のあと（または最近）新しい依頼が聞き取れなかったので念のため送らずにおいたこと、やる場合は{{if .Resend}}もう一度言うか、画面の「このまま送信」を押せばよいこと{{else}}もう一度言えばよいこと{{end}}を、一文でユーザーに伝えてください。ほかの人が言ったかのような言い方はしないでください。
{{- else if eq .Code "empty" -}}
{{$to}}に何を頼むのか聞き取れなかったので、何も送っていません。もう一度言ってもらうよう一文でユーザーに頼んでください。
{{- else if eq .Code "unknown" -}}
「{{.Task}}」が{{$to}}に届いたかわかりません：送信の途中で接続が切れました。もう一度頼まず、スマホで確認するよう一文でユーザーに伝えてください。
{{- else if eq .Code "unreachable" -}}
「{{.Task}}」は{{$to}}に送っていません：Persona につながりませんでした。送れなかったことを一文でユーザーに伝えてください。
{{- else if eq .Code "on_screen" -}}
「{{.Task}}」は{{$to}}に送っていません：はい・いいえだけの言葉で、{{$to}}はカード（計画、許可）を待っています。カードは声ではなく画面で押して答えます。画面で押すよう一文でユーザーに伝えてください。
{{- else if eq .Code "dialog_open" -}}
「{{.Task}}」は{{$to}}に送っていません：{{if .Plan}}計画が画面で承認を待っています。いま言ったことは、画面で押せば計画への修正意見として返せます{{else}}ターミナルのダイアログが先に対応を待っています（画面で）{{end}}。何も入力していません。一文でユーザーに伝えてください。
{{- else if eq .Code "input_draft" -}}
「{{.Task}}」は{{$to}}に送っていません：パソコン側の入力欄に、そこで打って送っていない文字が残っています。何も入力していません。一文でユーザーに伝えてください。画面で消して送ることができます。
{{- else if .Reason -}}
「{{.Task}}」は{{$to}}に送っていません：{{.Reason}}
送れなかったことと理由を一文でユーザーに伝えてください。
{{- else -}}
「{{.Task}}」は{{$to}}に送っていません。送れなかったことを一文でユーザーに伝えてください。
{{- end -}}
{{- else -}}
{{- $to := "Persona 主对话"}}{{if .Agent}}{{$to = "Agent"}}{{end}}{{with .To}}{{$to = .}}{{end -}}
{{- if eq .Code "unprompted" -}}
「{{.Task}}」没有发给 {{$to}}：上次把 Agent 做的事告诉你以后，没听到用户提出新的请求（或者用户上一次说话已经是半分钟以前），为了安全先没发。用一句话告诉用户：上次的结果之后（或者最近）没听到你新的请求，为了安全先没发；要做的话{{if .Resend}}再说一遍，或者点屏幕上的「仍然发送」{{else}}请再说一遍{{end}}。不要说得像是别人说的。
{{- else if eq .Code "empty" -}}
没听清要交给 {{$to}}的是什么，没有发出去。用一句话请用户再说一遍。
{{- else if eq .Code "unknown" -}}
「{{.Task}}」不确定有没有发到 {{$to}}：发送的途中连接断了。不要再交代一次，用一句话请用户去手机上看一下。
{{- else if eq .Code "unreachable" -}}
「{{.Task}}」没有发给 {{$to}}：连不上 Persona。用一句话告诉用户没发出去。
{{- else if eq .Code "on_screen" -}}
「{{.Task}}」没有发给 {{$to}}：这只是一句同意或不同意，而 {{$to}} 在等一张卡片（计划、授权）。卡片要在屏幕上点，不能用语音答。用一句话请用户在屏幕上点。
{{- else if eq .Code "dialog_open" -}}
「{{.Task}}」没有发给 {{$to}}：{{if .Plan}}它写好的计划在屏幕上等批准，用户可以在屏幕上点一下，把刚才这句作为修改意见退回给它{{else}}它的终端里有个对话框，要先在屏幕上处理{{end}}。什么都没输入。用一句话告诉用户。
{{- else if eq .Code "input_draft" -}}
「{{.Task}}」没有发给 {{$to}}：它在电脑上的输入框里有没发出去的字。什么都没输入。用一句话告诉用户，屏幕上可以清空后再发。
{{- else if .Reason -}}
「{{.Task}}」没有发给 {{$to}}：{{.Reason}}
用一句话告诉用户没发出去，以及原因。
{{- else -}}
「{{.Task}}」没有发给 {{$to}}。用一句话告诉用户没发出去。
{{- end -}}
{{- end -}}
