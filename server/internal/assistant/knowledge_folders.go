package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/devices"
	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

const (
	knowledgeSearchTool = "kb_folder_search"
	knowledgeReadTool   = "kb_folder_read"
)

func isKnowledgeFolderTool(name string) bool {
	return name == knowledgeSearchTool || name == knowledgeReadTool
}

func (s *Service) remoteKnowledgeFolders(ctx context.Context) []assistantwire.KBRemoteFolder {
	if set := s.kbSettings(ctx); set != nil && !set.Off {
		return set.RemoteFolders
	}
	return nil
}

func (s *Service) knowledgeFolderTools(ctx context.Context) []proto.Tool {
	if len(s.remoteKnowledgeFolders(ctx)) == 0 {
		return nil
	}
	return toolsNamed([]string{knowledgeSearchTool, knowledgeReadTool})
}

func (s *Service) toolKnowledgeFolder(ctx context.Context, name string, args json.RawMessage) (string, error) {
	var a struct {
		Source string `json:"source"`
		Query  string `json:"query"`
		Path   string `json:"path"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	folders := s.remoteKnowledgeFolders(ctx)
	if len(folders) == 0 {
		return "", errors.New("no remote knowledge folders are configured")
	}
	if name == knowledgeSearchTool && a.Source == "" && a.Query == "" {
		names := make([]string, 0, len(folders))
		for _, folder := range folders {
			names = append(names, folder.Name)
		}
		data, err := json.Marshal(names)
		return string(data), err
	}
	var folder *assistantwire.KBRemoteFolder
	for i := range folders {
		if a.Source != "" && folders[i].Name == a.Source {
			if folder != nil {
				return "", fmt.Errorf("knowledge folder name %q is configured more than once", a.Source)
			}
			folder = &folders[i]
		}
	}
	if folder == nil {
		return "", fmt.Errorf("unknown knowledge folder %q; call %s with no arguments to list sources", a.Source, knowledgeSearchTool)
	}
	if folder.Machine == "" || folder.Machine == DeviceAll || folder.Path == "" || s.isWorkMachine(folder.Machine) {
		return "", fmt.Errorf("knowledge folder %q needs a folder on a personal computer", a.Source)
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var err error
	var result any
	action := "be read"
	switch name {
	case knowledgeSearchTool:
		action = "be searched"
		if strings.TrimSpace(a.Query) == "" {
			return "", errors.New("knowledge folder search needs a query")
		}
		if a.Limit <= 0 {
			a.Limit = 40
		}
		request := devicewire.KnowledgeSearchRequest{KnowledgeFolder: folder.KnowledgeFolder, Query: a.Query, Limit: a.Limit}
		var out devicewire.KnowledgeSearchResult
		err = s.call(ctx, folder.Machine, devicewire.KnowledgeSearch, request, &out)
		result = out
	case knowledgeReadTool:
		request := devicewire.KnowledgeReadRequest{KnowledgeFolder: folder.KnowledgeFolder, Path: a.Path}
		var out devicewire.KnowledgeReadResult
		err = s.call(ctx, folder.Machine, devicewire.KnowledgeRead, request, &out)
		result = out
	}
	if errors.Is(err, apierr.ErrOffline) || errors.Is(err, rpc.ErrClosed) || errors.Is(err, ErrNoDevice) ||
		errors.Is(err, devices.ErrNoMachine) || errors.Is(err, context.DeadlineExceeded) {
		return "", fmt.Errorf("knowledge folder %q is unreachable; check that its computer is connected", a.Source)
	}
	if err != nil {
		return "", fmt.Errorf("knowledge folder %q could not %s; check the folder, file permissions and node version", a.Source, action)
	}
	data, err := json.Marshal(result)
	return string(data), err
}
