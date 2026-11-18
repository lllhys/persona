package assistant

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type repositoryPage struct {
	CatalogRevision int64               `json:"catalog_revision"`
	Repositories    []RepositoryBinding `json:"repositories"`
	NextCursor      string              `json:"next_cursor,omitempty"`
}

type repositoryCursor struct {
	Revision int64  `json:"revision"`
	Query    string `json:"query"`
	After    string `json:"after"`
}

func repositoryArguments(raw []byte, out any) error {
	if len(raw) > 16<<10 {
		return errors.New("repository arguments exceed the limit")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return errors.New("repository arguments must be an object")
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("repository arguments must not be null")
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return errors.New("invalid repository arguments")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("invalid repository arguments")
	}
	return nil
}

// toolRepositories has no network dependency: the manager's saved Settings are
// its only data source, regardless of the model, runtime, launcher or machine.
func (s *Service) toolRepositories(ctx context.Context, name string, raw json.RawMessage) (string, error) {
	if name == "repository_get" {
		var in struct {
			ID string `json:"id"`
		}
		if err := repositoryArguments(raw, &in); err != nil {
			return "", err
		}
		if in.ID == "" || len(in.ID) > 80 {
			return "", errors.New("give a saved repository id")
		}
		binding, err := s.repositoryByID(ctx, in.ID)
		if err != nil {
			return "", err
		}
		out, err := json.Marshal(binding)
		return string(out), err
	}
	var in struct {
		Query  string `json:"query"`
		Cursor string `json:"cursor"`
		Limit  *int   `json:"limit"`
	}
	if err := repositoryArguments(raw, &in); err != nil {
		return "", err
	}
	if !repositoryText(in.Query, 200) || len(in.Cursor) > 1024 {
		return "", errors.New("repository search exceeds the limit")
	}
	limit := 20
	if in.Limit != nil {
		limit = *in.Limit
	}
	if limit < 1 || limit > 50 {
		return "", errors.New("limit must be between 1 and 50")
	}
	query := strings.ToLower(strings.TrimSpace(in.Query))
	doc, err := s.repositoryDocument(ctx)
	if err != nil {
		return "", err
	}
	start := 0
	if in.Cursor != "" {
		var cursor repositoryCursor
		decoded, err := base64.RawURLEncoding.DecodeString(in.Cursor)
		if err != nil || repositoryArguments(decoded, &cursor) != nil || cursor.Revision != doc.Revision || cursor.Query != query || cursor.After == "" {
			return "", errors.New("invalid or stale repository cursor; restart without a cursor")
		}
		found := false
		for index, binding := range doc.Repositories {
			if binding.ID == cursor.After {
				start, found = index+1, true
				break
			}
		}
		if !found {
			return "", errors.New("invalid repository cursor")
		}
	}
	page := repositoryPage{CatalogRevision: doc.Revision, Repositories: []RepositoryBinding{}}
	for _, binding := range doc.Repositories[start:] {
		matches := query == ""
		for _, value := range append([]string{binding.Name, binding.Path, binding.Purpose}, binding.Aliases...) {
			matches = matches || strings.Contains(strings.ToLower(value), query)
		}
		if !matches {
			continue
		}
		if len(page.Repositories) == limit {
			cursor, _ := json.Marshal(repositoryCursor{doc.Revision, query, page.Repositories[len(page.Repositories)-1].ID})
			page.NextCursor = base64.RawURLEncoding.EncodeToString(cursor)
			break
		}
		page.Repositories = append(page.Repositories, binding)
	}
	out, err := json.Marshal(page)
	return string(out), err
}
