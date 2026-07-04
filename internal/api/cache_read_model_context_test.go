package api

import (
	"context"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/session"
)

// contextOnlyListStore is a beads.Store + beads.ContextLister fake that does
// NOT implement cachedListStore, so listSessionBeadsForReadModelContext must
// fall through to session.ListAllSessionBeadsContext.
type contextOnlyListStore struct {
	beads.Store
	calls []context.Context
}

func (s *contextOnlyListStore) ListContext(ctx context.Context, query beads.ListQuery) ([]beads.Bead, error) {
	s.calls = append(s.calls, ctx)
	return s.List(query)
}

func TestListSessionBeadsForReadModelContext_DelegatesToContextListerWhenCacheMisses(t *testing.T) {
	mem := beads.NewMemStore()
	if _, err := mem.Create(beads.Bead{
		Title:  "session",
		Type:   session.BeadType,
		Labels: []string{session.LabelSession},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	store := &contextOnlyListStore{Store: mem}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rows, err := listSessionBeadsForReadModelContext(ctx, store)
	if err != nil {
		t.Fatalf("listSessionBeadsForReadModelContext: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if len(store.calls) == 0 {
		t.Fatal("ListContext was never called; want delegation via ContextLister")
	}
	for i, gotCtx := range store.calls {
		if gotCtx != ctx {
			t.Errorf("call %d: ctx = %v, want the caller's ctx", i, gotCtx)
		}
	}
}

func TestListSessionBeadsForReadModelContext_ServedFromCacheFastPath(t *testing.T) {
	backing := beads.NewMemStore()
	if _, err := backing.Create(beads.Bead{
		Title:  "session",
		Type:   session.BeadType,
		Labels: []string{session.LabelSession},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	cache := beads.NewCachingStoreForTest(backing, nil)
	if err := cache.Prime(context.Background()); err != nil {
		t.Fatalf("Prime: %v", err)
	}

	// A canceled ctx must not matter: the cache fast path answers entirely
	// in-memory, mirroring listSessionBeadsForReadModel's own behavior.
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	rows, err := listSessionBeadsForReadModelContext(canceledCtx, cache)
	if err != nil {
		t.Fatalf("listSessionBeadsForReadModelContext: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (served from cache, ctx cancellation irrelevant)", len(rows))
	}
}

func TestSessionReadModelRowsContext_WrapsPartialResultAsWarning(t *testing.T) {
	mem := beads.NewMemStore()
	survivor, err := mem.Create(beads.Bead{
		Title:  "session survivor",
		Type:   session.BeadType,
		Labels: []string{session.LabelSession},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	backing := &partialPrimeSessionStore{MemStore: mem}
	backing.partialRows = []beads.Bead{survivor}
	cache := beads.NewCachingStoreForTest(backing, nil)
	if err := cache.Prime(context.Background()); err != nil {
		t.Fatalf("Prime: %v", err)
	}

	rows, partialErrors, err := sessionReadModelRowsContext(context.Background(), cache)
	if err != nil {
		t.Fatalf("sessionReadModelRowsContext: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 survivor row", len(rows))
	}
	if len(partialErrors) == 0 {
		t.Fatal("want a partial-result warning surfaced, got none")
	}
}
