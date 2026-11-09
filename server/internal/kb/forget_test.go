package kb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Taking a source back removes all of it and nothing else: its chats and people, their places in a project and the
// timeline lines its chats wrote; the project is written again from what is left.
func TestForgetSource(t *testing.T) {
	e := testEngine(t, nil)
	st := e.Store
	at := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	_ = st.PutChat(&Chat{ID: "lark-f1", Source: "lark", Kind: "group", Name: "需求群"})
	_ = st.PutChat(&Chat{ID: "imessage-0123456789ab", Source: "imessage", Kind: "dm", Name: "张三"})
	_ = st.PutChat(&Chat{ID: "imessage-ba9876543210", Source: "imessage", Kind: "group", Name: "周末徒步"})
	_ = st.PutSummary(KindChat, "imessage-0123456789ab", "# 张三\n\n示例：周五见")
	_ = st.PutPerson(&Person{ID: "lark-u1", Source: "lark", Name: "甲"})
	_ = st.PutPerson(&Person{ID: "imessage-a1b2c3d4e5f6", Source: "imessage", Name: "张三"})
	_ = st.PutProject(&Project{Slug: "x", Name: "X 上线", Status: "active", Chats: []string{"lark-f1", "imessage-0123456789ab"},
		People: []string{"lark-u1", "imessage-a1b2c3d4e5f6"}})
	_ = st.PutProject(&Project{Slug: "y", Name: "别的", Status: "active", Chats: []string{"lark-f1"}})
	_ = st.AppendLog("x", at, "需求群", "定了周五灰度")
	_ = st.AppendLog("x", at.Add(time.Hour), "张三", "示例：周五见面细聊")
	_ = os.MkdirAll(filepath.Join(st.Dir, "cursors"), 0o700)
	_ = os.WriteFile(filepath.Join(st.Dir, "cursors", "imessage.json"), []byte(`{"cursor":"42"}`), 0o600)
	_ = os.WriteFile(filepath.Join(st.Dir, "cursors", "github.json"), []byte(`{"cursor":"x"}`), 0o600)

	got, err := e.ForgetSource("imessage")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Chats, ",") != "imessage-0123456789ab,imessage-ba9876543210" && strings.Join(got.Chats, ",") != "imessage-ba9876543210,imessage-0123456789ab" ||
		strings.Join(got.People, ",") != "imessage-a1b2c3d4e5f6" || strings.Join(got.Projects, ",") != "x" {
		t.Fatalf("forgot %+v", got)
	}
	if _, ok := st.Chat("imessage-0123456789ab"); ok {
		t.Error("the chat is still there")
	}
	if _, err := os.Stat(st.Path(KindChat, "imessage-0123456789ab")); !os.IsNotExist(err) {
		t.Errorf("its directory: %v", err)
	}
	if _, ok := st.Person("imessage-a1b2c3d4e5f6"); ok {
		t.Error("the person is still there")
	}
	if _, ok := st.Chat("lark-f1"); !ok {
		t.Error("Feishu's chat is gone")
	}
	if _, ok := st.Person("lark-u1"); !ok {
		t.Error("Feishu's person is gone")
	}
	x, _ := st.Project("x")
	if strings.Join(x.Chats, ",") != "lark-f1" || strings.Join(x.People, ",") != "lark-u1" || !x.Dirty {
		t.Errorf("project x = %+v", x)
	}
	if log := st.Log("x", 10); len(log) != 1 || !strings.Contains(log[0], "定了周五灰度") {
		t.Errorf("timeline = %v", log)
	}
	if y, _ := st.Project("y"); y.Dirty {
		t.Error("an untouched project was marked dirty")
	}
	if _, err := os.Stat(filepath.Join(st.Dir, "cursors", "imessage.json")); !os.IsNotExist(err) {
		t.Errorf("cursor: %v", err)
	}
	if _, err := os.Stat(filepath.Join(st.Dir, "cursors", "github.json")); err != nil {
		t.Errorf("another connector's cursor: %v", err)
	}
	// Again: nothing left to forget.
	if again, err := e.ForgetSource("imessage"); err != nil || len(again.Chats)+len(again.People)+len(again.Projects) != 0 {
		t.Errorf("again %+v %v", again, err)
	}
}
