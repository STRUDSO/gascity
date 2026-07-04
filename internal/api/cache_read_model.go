package api

import (
	"context"
	"sort"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/session"
)

type cachedListStore interface {
	CachedList(beads.ListQuery) ([]beads.Bead, bool)
}

// sessionBeadsFromCache is the cache-hit fast path shared by
// listSessionBeadsForReadModel and its ctx-aware sibling: it asks the cache
// for both the type and label query shapes the union helper would issue and
// merges them locally if both hit. This is pure in-memory work, so it never
// needs a context. The boolean reports whether the cache answered.
func sessionBeadsFromCache(store beads.Store) ([]beads.Bead, bool) {
	cached, ok := store.(cachedListStore)
	if !ok {
		return nil, false
	}
	typeQuery := beads.ListQuery{Type: session.BeadType, Sort: beads.SortCreatedDesc}
	labelQuery := beads.ListQuery{Label: session.LabelSession, Sort: beads.SortCreatedDesc}
	typeRows, typeOK := cached.CachedList(typeQuery)
	labelRows, labelOK := cached.CachedList(labelQuery)
	if !typeOK || !labelOK {
		return nil, false
	}
	seen := make(map[string]struct{}, len(typeRows)+len(labelRows))
	merged := make([]beads.Bead, 0, len(typeRows)+len(labelRows))
	for _, b := range typeRows {
		if _, dup := seen[b.ID]; dup {
			continue
		}
		if !session.IsSessionBeadOrRepairable(b) {
			continue
		}
		seen[b.ID] = struct{}{}
		merged = append(merged, b)
	}
	for _, b := range labelRows {
		if _, dup := seen[b.ID]; dup {
			continue
		}
		if !session.IsSessionBeadOrRepairable(b) {
			continue
		}
		seen[b.ID] = struct{}{}
		merged = append(merged, b)
	}
	// Match the helper's global sort — the query is hardcoded to
	// SortCreatedDesc, so cached and uncached paths must agree on order
	// across mixed-shape rows.
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].CreatedAt.After(merged[j].CreatedAt)
	})
	return merged, true
}

func listSessionBeadsForReadModel(store beads.Store) ([]beads.Bead, error) {
	if merged, ok := sessionBeadsFromCache(store); ok {
		return merged, nil
	}
	return session.ListAllSessionBeads(store, beads.ListQuery{Sort: beads.SortCreatedDesc})
}

// listSessionBeadsForReadModelContext is listSessionBeadsForReadModel but
// accepts a context so the status endpoint's session snapshot can cancel the
// backing queries instead of leaking a goroutine on timeout. The cache-hit
// fast path is unchanged (in-memory, ctx-independent); only the backing
// delegation on a cache miss is ctx-aware, via ListAllSessionBeadsContext.
func listSessionBeadsForReadModelContext(ctx context.Context, store beads.Store) ([]beads.Bead, error) {
	if merged, ok := sessionBeadsFromCache(store); ok {
		return merged, nil
	}
	return session.ListAllSessionBeadsContext(ctx, store, beads.ListQuery{Sort: beads.SortCreatedDesc})
}

func sessionReadModelRows(store beads.Store) ([]beads.Bead, []string, error) {
	rows, err := listSessionBeadsForReadModel(store)
	if err == nil {
		return rows, nil, nil
	}
	if beads.IsPartialResult(err) && len(rows) > 0 {
		return rows, []string{err.Error()}, nil
	}
	return nil, nil, err
}

// sessionReadModelRowsContext is sessionReadModelRows but accepts a context;
// used only by the status endpoint's session snapshot (statusSessionSnapshot).
func sessionReadModelRowsContext(ctx context.Context, store beads.Store) ([]beads.Bead, []string, error) {
	rows, err := listSessionBeadsForReadModelContext(ctx, store)
	if err == nil {
		return rows, nil, nil
	}
	if beads.IsPartialResult(err) && len(rows) > 0 {
		return rows, []string{err.Error()}, nil
	}
	return nil, nil, err
}
