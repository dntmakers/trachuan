package frameworkaccess

import (
	"context"
	"sync"

	"danny.vn/trachuan/apps/server/internal/store"
)

type requestKey struct{}

type requestStore struct {
	mu        sync.Mutex
	snapshots map[int64]store.FrameworkAccessSnapshot
}

func RequestContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestKey{}, &requestStore{snapshots: make(map[int64]store.FrameworkAccessSnapshot)})
}

func RequestStore(ctx context.Context, source Store) Store {
	state, ok := ctx.Value(requestKey{}).(*requestStore)
	if !ok {
		return source
	}
	return &requestAccessStore{state: state, source: source}
}

type requestAccessStore struct {
	state  *requestStore
	source Store
}

func (s *requestAccessStore) FrameworkAccessSnapshot(ctx context.Context, id int64) (store.FrameworkAccessSnapshot, error) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	if snapshot, ok := s.state.snapshots[id]; ok {
		return snapshot, nil
	}
	snapshot, err := s.source.FrameworkAccessSnapshot(ctx, id)
	if err == nil {
		s.state.snapshots[id] = snapshot
	}
	return snapshot, err
}
