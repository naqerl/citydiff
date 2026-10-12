package observe

import (
	"context"
	"sync"
)

// OverviewKey and RangeKey name a built scene. A three-dot range is keyed by
// the commits it resolves to, which already start at the merge base.
func OverviewKey(head string) string { return "overview " + head }
func RangeKey(left, right string) string {
	return "left " + left + " right " + right
}

// Cache keeps one overview and a fixed number of range scenes per bucket,
// and runs at most one build of a key, with a bound on builds in flight.
// The bucket is the clone: two observations of one remote share the scenes.
type Cache[T any] struct {
	ranges  int
	sem     chan struct{}
	mu      sync.Mutex
	buckets map[string]*bucket[T]
	flights map[string]*flight[T]
}

type bucket[T any] struct {
	overviewKey string
	overview    T
	hasOverview bool
	order       []string
	items       map[string]T
}

type flight[T any] struct {
	done chan struct{}
	val  T
	err  error
}

// NewCache keeps ranges range scenes per bucket, besides the overview, and
// runs at most builds builds at once.
func NewCache[T any](ranges, builds int) *Cache[T] {
	if ranges < 1 {
		ranges = 8
	}
	if builds < 1 {
		builds = 1
	}
	return &Cache[T]{
		ranges:  ranges,
		sem:     make(chan struct{}, builds),
		buckets: map[string]*bucket[T]{},
		flights: map[string]*flight[T]{},
	}
}

// Get returns the cached value for key, building it when it is missing.
// overview values are kept on their own and are not pushed out by ranges.
func (c *Cache[T]) Get(ctx context.Context, bucket, key string, overview bool, build func(context.Context) (T, error)) (T, error) {
	id := bucket + "\x00" + key
	for {
		c.mu.Lock()
		if b := c.buckets[bucket]; b != nil {
			if v, ok := b.lookup(key, overview); ok {
				c.mu.Unlock()
				return v, nil
			}
		}
		if f := c.flights[id]; f != nil {
			c.mu.Unlock()
			select {
			case <-f.done:
				return f.val, f.err
			case <-ctx.Done():
				var zero T
				return zero, ctx.Err()
			}
		}
		f := &flight[T]{done: make(chan struct{})}
		c.flights[id] = f
		c.mu.Unlock()
		return c.run(ctx, id, bucket, key, overview, build, f)
	}
}

func (c *Cache[T]) run(ctx context.Context, id, name, key string, overview bool, build func(context.Context) (T, error), f *flight[T]) (val T, err error) {
	defer func() {
		c.mu.Lock()
		f.val, f.err = val, err
		delete(c.flights, id)
		close(f.done)
		c.mu.Unlock()
	}()
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
	defer func() { <-c.sem }()
	val, err = build(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	c.mu.Lock()
	c.store(name, key, overview, val)
	c.mu.Unlock()
	return val, nil
}

func (c *Cache[T]) store(name, key string, overview bool, val T) {
	b := c.buckets[name]
	if b == nil {
		b = &bucket[T]{items: map[string]T{}}
		c.buckets[name] = b
	}
	if overview {
		b.overviewKey = key
		b.overview = val
		b.hasOverview = true
		return
	}
	if _, ok := b.items[key]; ok {
		b.items[key] = val
		b.touch(key)
		return
	}
	b.items[key] = val
	b.order = append(b.order, key)
	for len(b.order) > c.ranges {
		old := b.order[0]
		b.order = b.order[1:]
		delete(b.items, old)
	}
}

func (b *bucket[T]) lookup(key string, overview bool) (T, bool) {
	if overview {
		if b.hasOverview && b.overviewKey == key {
			return b.overview, true
		}
		var zero T
		return zero, false
	}
	v, ok := b.items[key]
	if ok {
		b.touch(key)
	}
	return v, ok
}

func (b *bucket[T]) touch(key string) {
	for i, k := range b.order {
		if k == key {
			b.order = append(append(b.order[:i], b.order[i+1:]...), key)
			return
		}
	}
}

// Invalidate drops every cached value for bucket, so the next Get rebuilds
// them. The bucket is the clone, so every observation sharing the clone is
// invalidated when the clone's refs move.
func (c *Cache[T]) Invalidate(bucket string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.buckets, bucket)
}

// Has reports whether key is cached. It is for tests.
func (c *Cache[T]) Has(bucket, key string, overview bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.buckets[bucket]
	if b == nil {
		return false
	}
	_, ok := b.lookup(key, overview)
	return ok
}
