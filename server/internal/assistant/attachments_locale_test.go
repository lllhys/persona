package assistant

import (
	"context"
	"testing"
)

func TestMissingAttachmentDetailFollowsUserLanguage(t *testing.T) {
	for _, tt := range []struct{ locale, want string }{
		{"en", "/tmp/missing.txt (no such file)"},
		{"zh-Hans", "/tmp/missing.txt（no such file）"},
		{"ja", "/tmp/missing.txt（no such file）"},
	} {
		t.Run(tt.locale, func(t *testing.T) {
			s, f := newTaskService(t)
			f.mu.Lock()
			f.hello.Locale = tt.locale
			f.sessions["server"] = nil
			f.mu.Unlock()
			newFakeFiles(f, nil)
			h := s.handAttachments(context.Background(), "server", "box", []string{"/tmp/missing.txt"}, nil)
			if len(h.Missing) != 1 || h.Missing[0] != tt.want {
				t.Fatalf("missing detail: %v want %s", h.Missing, tt.want)
			}
		})
	}
}

func TestAttachmentPathDelimitersMatchOriginalPunctuation(t *testing.T) {
	for _, delimiter := range []string{" ", "（", "：", "，", "、", "\""} {
		for _, ending := range []string{" ", "）", "，", "。", "；", "、", "\""} {
			got := textPath.FindAllStringSubmatch("file"+delimiter+"/tmp/upload.txt"+ending+"next", -1)
			if len(got) != 1 || len(got[0]) != 2 || got[0][1] != "/tmp/upload.txt" {
				t.Fatalf("delimiters %q %q: %v", delimiter, ending, got)
			}
		}
	}
}
