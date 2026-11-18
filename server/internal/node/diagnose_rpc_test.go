package node

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// machine.diagnose and machine.cleanup reach machdiag, which refuses what is not in its fixed lists (#8037).
func TestMachineDiagnoseDispatch(t *testing.T) {
	n := &Node{}
	for _, c := range []struct{ method, params string }{
		{proto.MethodMachineDiagnose, `{"sections":["environment"]}`},
		{proto.MethodMachineCleanup, `{"ids":["/"]}`},
		{proto.MethodMachineCleanup, `{}`},
	} {
		_, err := n.handle(context.Background(), c.method, json.RawMessage(c.params))
		var re *rpc.Error
		if !errors.As(err, &re) || re.Code != "bad_request" {
			t.Errorf("%s %s: %v", c.method, c.params, err)
		}
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return
	}
	out, err := n.handle(context.Background(), proto.MethodMachineDiagnose, json.RawMessage(`{"sections":["processes"]}`))
	if err != nil {
		t.Fatal(err)
	}
	d, ok := out.(proto.Diagnosis)
	if !ok || d.Processes == nil || d.Processes.Count == 0 || d.Disk != nil || d.Tools != nil {
		t.Fatalf("diagnosis = %#v", out)
	}
}
