package repository

import (
	"encoding/json"
	"os"
	"testing"
)

func TestTaskRepositorySharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../../spec/fixtures/task-repositories/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Catalog  Catalog                           `json:"catalog"`
		Input    []TaskInput                       `json:"input"`
		Selected []string                          `json:"selected"`
		Revision int64                             `json:"snapshot_revision"`
		Digest   string                            `json:"snapshot_digest"`
		Aliases  struct{ Valid, Invalid []string } `json:"aliases"`
		Refs     []RequestedRef                    `json:"refs"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	rows, err := Bind(nil, fixture.Input, fixture.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := Snapshot("assistant", "todo_example", fixture.Revision, rows, fixture.Selected)
	if err != nil || snapshot.Digest != fixture.Digest {
		t.Fatal(snapshot, err)
	}
	// Link edits and association revision changes do not alter physical preparation inputs.
	rows[0].Links = nil
	other, err := Snapshot("assistant", "todo_example", fixture.Revision+1, rows, fixture.Selected)
	if err != nil || other.Digest != snapshot.Digest {
		t.Fatal("link revision changed physical digest", other, err)
	}
	for _, alias := range fixture.Aliases.Valid {
		if err := ValidateAlias(alias); err != nil {
			t.Fatal(alias, err)
		}
	}
	for _, alias := range fixture.Aliases.Invalid {
		if ValidateAlias(alias) == nil {
			t.Fatal("accepted", alias)
		}
	}
	for _, ref := range fixture.Refs {
		if _, err := NormalizeRef(ref); err != nil {
			t.Fatal(ref, err)
		}
	}
}
