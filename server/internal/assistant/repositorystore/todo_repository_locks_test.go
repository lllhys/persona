package repositorystore

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nwverse/persona/server/internal/assistant/repository"
	"github.com/nwverse/persona/server/internal/db"
)

func TestTodoRepositoryMutationsSerializeWithEditsAndCatalogUpdates(t *testing.T) {
	for _, operation := range []string{"repositories", "links", "import"} {
		t.Run(operation, func(t *testing.T) {
			d := open(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			one, two := int64(1), int64(2)
			identity, err := repository.Parse("https://github.com/example/web", "")
			if err != nil {
				t.Fatal(err)
			}
			catalog := repository.Catalog{Revision: one, Repositories: []repository.CatalogEntry{{ID: "web", Identity: identity}}}
			if err := d.PutSetting(ctx, repository.CatalogKey, catalog); err != nil {
				t.Fatal(err)
			}
			const id = "repository-lock-order"
			links := []repository.LinkInput{{Kind: "pull_request", Number: "9", Title: "Keep task context"}}
			rows := []repository.TaskInput{{RepoID: "web", Links: links}}
			if err := d.CreateTodoWithRepositories(ctx, db.TodoRow{ID: id, Title: "Original", CreatedAt: time.Now(), UpdatedAt: time.Now()}, repository.CreateBindings{CatalogRevision: &one, Repositories: rows}); err != nil {
				t.Fatal(err)
			}
			conn, err := pgx.Connect(ctx, os.Getenv("PERSONA_TEST_DATABASE_URL"))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close(context.Background())
			guard, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Rollback(context.Background())
			if _, err := guard.Exec(ctx, `SELECT id FROM todos WHERE id=$1 FOR UPDATE`, id); err != nil {
				t.Fatal(err)
			}
			results := make(chan error, 2)
			finished := 0
			defer func() {
				_ = guard.Rollback(context.Background())
				cancel()
				for finished < 2 {
					select {
					case <-results:
						finished++
					case <-time.After(5 * time.Second):
						t.Error("database writers did not stop")
						return
					}
				}
			}()
			go func() {
				title := "Edited concurrently"
				_, err := d.EditTodo(ctx, id, db.TodoEdit{Title: &title}, time.Now())
				results <- err
			}()
			go func() {
				var err error
				switch operation {
				case "repositories":
					_, err = d.SetTodoRepositories(ctx, id, repository.SetInput{ExpectedRevision: &one, ExpectedCatalogRevision: &one, Repositories: rows})
				case "links":
					_, err = d.SetTodoRepositoryLinks(ctx, id, "web", repository.LinksInput{ExpectedRevision: &one, Links: links})
				case "import":
					_, _, err = d.ImportTodoRepositoryLinkDetails(ctx, id, []repository.LinkInput{{URL: "https://github.com/example/web/pull/10", Title: "Retain the workspace"}}, "explicit")
				}
				results <- err
			}()
			// Both real API writers must first wait for the Todo, without holding the catalog.
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				var waiting int
				if err := d.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
					WHERE datname=current_database() AND wait_event_type='Lock'
					AND query LIKE 'SELECT%FROM todos%FOR UPDATE%'`).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting == 2 {
					break
				}
				select {
				case <-ticker.C:
				case <-ctx.Done():
					t.Fatal("writers did not reach their Todo lock waits")
				}
			}
			// A catalog writer can commit while both operations wait for the Todo. This would
			// time out if either path acquired the catalog lock in the opposite order.
			catalog.Revision = two
			catalogCtx, stop := context.WithTimeout(ctx, 2*time.Second)
			changed, err := d.CompareSetting(catalogCtx, repository.CatalogKey, one, catalog)
			stop()
			if err != nil || !changed {
				t.Fatal("a Todo waiter held the catalog", changed, err)
			}
			if err := guard.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			conflicts := 0
			for finished < 2 {
				select {
				case err := <-results:
					finished++
					if operation == "repositories" && errors.Is(err, repository.Problem("catalog_conflict")) {
						conflicts++
					} else if err != nil {
						t.Fatal("concurrent edit or association mutation failed", err)
					}
				case <-ctx.Done():
					t.Fatal("writers failed to finish after the Todo was unlocked")
				}
			}
			if operation == "repositories" {
				if conflicts != 1 {
					t.Fatal("association save missed the concurrent catalog change", conflicts)
				}
				if _, err := d.SetTodoRepositories(ctx, id, repository.SetInput{ExpectedRevision: &one, ExpectedCatalogRevision: &two, Repositories: rows}); err != nil {
					t.Fatal("save using the current catalog revision failed", err)
				}
			}
			todo, err := d.GetTodo(ctx, id)
			if err != nil || todo.Title != "Edited concurrently" {
				t.Fatal("association mutation lost the ordinary edit", todo, err)
			}
			set, err := d.TodoRepositories(ctx, id)
			if err != nil || set.Revision != two || len(set.Repositories) != 1 || len(set.Repositories[0].Links) == 0 || set.Repositories[0].Links[0].Title != "Keep task context" {
				t.Fatal("concurrent edit lost associations or revision", set, err)
			}
			if operation == "import" && (len(set.Repositories[0].Links) != 2 || set.Repositories[0].Links[1].Title != "Retain the workspace") {
				t.Fatal("concurrent import lost its new link", set)
			}
		})
	}
}
