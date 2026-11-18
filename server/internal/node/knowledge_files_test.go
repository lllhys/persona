package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/devicewire"
)

func TestKnowledgeFolderRPC(t *testing.T) {
	home := t.TempDir()
	n := &Node{home: home, state: newState()}
	for _, name := range []string{"notes/doc.md", "notes/sub/doc.txt", "notes/private/secret.md", "notes/scan.PDF", "notes/id_ed25519", "outside.md"} {
		file := filepath.Join(home, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("Unique SEARCH terms"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	linkErr := os.Symlink(filepath.Join(home, "notes", "private"), filepath.Join(home, "notes", "alias"))
	folder := devicewire.KnowledgeFolder{Path: "~/notes", ExcludeDirs: []string{"private"}, ExcludeExtensions: []string{".pdf"}}
	call := func(method string, params any) (any, error) {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		return n.handle(context.Background(), method, raw)
	}
	out, err := call(devicewire.KnowledgeSearch, devicewire.KnowledgeSearchRequest{KnowledgeFolder: folder, Query: "search", Limit: 40})
	if err != nil {
		t.Fatal(err)
	}
	hits := out.(devicewire.KnowledgeSearchResult)
	if !slices.Equal(hits.Paths, []string{"doc.md", "sub/doc.txt"}) || hits.Truncated {
		t.Fatalf("search = %+v", hits)
	}
	out, err = call(devicewire.KnowledgeRead, devicewire.KnowledgeReadRequest{KnowledgeFolder: folder, Path: "sub/doc.txt"})
	if err != nil || out != (devicewire.KnowledgeReadResult{Text: "Unique SEARCH terms"}) {
		t.Fatalf("read = %+v, %v", out, err)
	}

	denied := []string{"private/secret.md", "../outside.md", "scan.PDF", "id_ed25519"}
	if linkErr == nil {
		denied = append(denied, "alias/secret.md")
	}
	for _, name := range denied {
		if _, err := call(devicewire.KnowledgeRead, devicewire.KnowledgeReadRequest{KnowledgeFolder: folder, Path: name}); err == nil {
			t.Errorf("allowed read of %q", name)
		}
	}
}

func TestKnowledgeFolderBounds(t *testing.T) {
	home := t.TempDir()
	n := &Node{home: home, state: newState()}
	folder := devicewire.KnowledgeFolder{Path: home}
	longText := strings.Repeat("a", knowledgeFileBytes-1) + "界"
	if err := os.WriteFile(filepath.Join(home, "one.txt"), []byte(longText), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "two.txt"), []byte("sample"), 0600); err != nil {
		t.Fatal(err)
	}
	read, err := n.readKnowledge(devicewire.KnowledgeReadRequest{KnowledgeFolder: folder, Path: "one.txt"})
	if err != nil || !read.Truncated || len(read.Text) != knowledgeFileBytes-1 || !utf8.ValidString(read.Text) {
		t.Fatalf("bounded read = %d bytes, truncated=%v, err=%v", len(read.Text), read.Truncated, err)
	}
	hits, err := n.searchKnowledge(context.Background(), devicewire.KnowledgeSearchRequest{KnowledgeFolder: folder, Query: "a", Limit: 1})
	if err != nil || !hits.Truncated || len(hits.Paths) != 1 {
		t.Fatalf("bounded search = %+v, err=%v", hits, err)
	}
	folder.ExcludeDirs = []string{"notes/../private"}
	if _, err := n.readKnowledge(devicewire.KnowledgeReadRequest{KnowledgeFolder: folder, Path: "one.txt"}); err == nil {
		t.Fatal("malformed exclusion allowed reads")
	}
}
