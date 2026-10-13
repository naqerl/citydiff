// Package par runs the independent work of a language parser across a bounded
// pool of workers.
//
// Every language parser has the same shape: parse each file into a draft, build
// one index over all drafts, then fill each entity's calls from that index.
// Only the first and the last phase are independent — the index build is a
// barrier between them — so a pool is enough and no lock is needed anywhere: a
// worker owns its state and writes its result by index, and the index is
// read-only while the calls are collected.
package par

import (
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
)

// Workers is the number of workers a phase may use. It is GOMAXPROCS, so a
// single-CPU box parses serially, and CITYDIFF_PARSER_WORKERS overrides it to
// force the serial path (1) or to bound the pool. The knob is internal and
// changes scheduling only: the output is a function of the source, not of the
// worker count.
func Workers() int {
	n := runtime.GOMAXPROCS(0)
	if v := os.Getenv("CITYDIFF_PARSER_WORKERS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			n = parsed
		}
	}
	if n < 1 {
		n = 1
	}
	return n
}

// Do calls fn for every index in [0,n) and returns when all of them have run.
// Each worker value is used by one goroutine at a time, so per-worker state
// such as a *tree_sitter.Parser — which is not thread-safe — stays single
// threaded. At most min(len(workers), n) of the workers are used, and a run
// that has no parallelism to spend (one worker, or no item) stays in the
// calling goroutine, which is the serial path.
//
// Indices are handed out in completion order, not in index order, so a caller
// must write fn's result into a slice indexed by i and must not append. The
// parser's output order is fixed by the drafts, never by when a worker
// finished.
func Do[W any](n int, workers []W, fn func(W, int)) {
	if n <= 0 {
		return
	}
	w := len(workers)
	if w > n {
		w = n
	}
	if w <= 1 {
		for i := 0; i < n; i++ {
			fn(workers[0], i)
		}
		return
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	wg.Add(w)
	for k := 0; k < w; k++ {
		go func(k int) {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				fn(workers[k], i)
			}
		}(k)
	}
	wg.Wait()
}

// For is Do for a phase that needs no per-worker state.
func For(n int, fn func(int)) {
	Do(n, make([]struct{}, Workers()), func(_ struct{}, i int) { fn(i) })
}

// Closer is what a per-worker value must be able to do when its phase ends.
type Closer interface{ Close() }

// CloseAll closes every value in order. Callers only pass values that were
// built successfully, so there is no nil to skip.
func CloseAll[P Closer](vals []P) {
	for _, v := range vals {
		v.Close()
	}
}
