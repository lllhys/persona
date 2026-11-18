package node

import "testing"

// Higher-order operator helpers can recover os.system from sys.modules while every dangerous name is inside a
// string literal. These harmless commands are only classified; neither the commands nor lark-cli is executed.
func TestInlinePythonHigherOrderOperatorRequiresApproval(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
	}{
		{
			"attrgetter and itemgetter",
			`import operator;import sys;operator.attrgetter("system")(operator.itemgetter("os")(operator.attrgetter("modules")(sys)))("lark-cli --version")`,
		},
		{
			"methodcaller only",
			`import operator;import sys;operator.methodcaller("system","lark-cli --version")(operator.methodcaller("__getitem__","os")(operator.methodcaller("__getattribute__","modules")(sys)))`,
		},
		{
			"from imports",
			`from operator import attrgetter, itemgetter;import sys;attrgetter("system")(itemgetter("os")(attrgetter("modules")(sys)))("lark-cli --version")`,
		},
		{
			"import aliases",
			`import operator as op;import sys as s;op.attrgetter("system")(op.itemgetter("os")(op.attrgetter("modules")(s)))("lark-cli --version")`,
		},
		{
			"from import alias",
			`from operator import methodcaller as mc;import sys;mc("system","lark-cli --version")(mc("__getitem__","os")(mc("__getattribute__","modules")(sys)))`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := `python3 -c '` + tc.payload + `'`
			if inertPythonCode([]string{"python3", "-c", tc.payload}) {
				t.Error("higher-order operator chain was classified as inert")
			}
			v := classifyLark(command, nil, "", nil)
			if !v.unknown {
				t.Errorf("higher-order operator chain is not unknown: %+v", v)
			}
			assertAssistantBypassClassification(t, command, 1)
		})
	}
}

func TestInlinePythonSafeOperatorRemainsInert(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
	}{
		{"operator add", `import operator;print(operator.add(1, 2), "lark-cli --version")`},
		{"sys version", `import sys;print(sys.version, "lark-cli --version")`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := `python3 -c '` + tc.payload + `'`
			if !inertPythonCode([]string{"python3", "-c", tc.payload}) {
				t.Error("safe control was not classified as inert")
			}
			v := classifyLark(command, nil, "", nil)
			if v.unknown {
				t.Errorf("safe control is unknown: %+v", v)
			}
			assertAssistantBypassClassification(t, command, 0)
		})
	}
}
