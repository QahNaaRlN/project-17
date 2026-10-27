package validation_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

// Edits share the project's environments; FOR UPDATE commands wait for them (load tests).
func TestSharedEnvironmentLocks(t *testing.T) {
	e := cmstest.New(t)
	ctx := context.Background()
	begin := func() pgx.Tx {
		tx, err := e.Pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tx.Rollback(ctx) })
		if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '300ms'"); err != nil {
			t.Fatal(err)
		}
		return tx
	}
	edit := begin()
	if err := validation.LockProjectEnvironments(validation.Shared(ctx), store.New(edit), e.Admin.ProjectID); err != nil {
		t.Fatal(err)
	}
	// Another edit proceeds, and so does a shared Load inside it.
	other := begin()
	shared := validation.Shared(ctx)
	if err := validation.LockProjectEnvironments(shared, store.New(other), e.Admin.ProjectID); err != nil {
		t.Fatal("edits must not wait for each other:", err)
	}
	if _, err := validation.Load(shared, store.New(other), e.Admin.ProjectID, "staging", nil); err != nil {
		t.Fatal(err)
	}
	// A publication-like command waits for the edits.
	if err := validation.LockProjectEnvironments(ctx, store.New(begin()), e.Admin.ProjectID); err == nil {
		t.Fatal("FOR UPDATE must wait for shared edits")
	}
}
