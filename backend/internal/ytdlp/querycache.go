package ytdlp

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"ytdm/backend/internal/apperr"
)

// Metadata query reuse.
//
// Every yt-dlp process of a managed YouTube session passes the session's
// execution gate and its start-rate pacing, so every avoidable process costs
// session time and provider quota. Before this cache the very same single-item
// extraction ran several times inside one attempt - the direct-id probe and the
// resolve of that candidate, the runtime enrichment and the later resolve - and
// again for every retry and for every item that shares a candidate.
//
// Only the outcome of an identical invocation is reused: same binary, same
// cookie file, same player clients, same arguments. That keeps providers and
// session contexts apart without any knowledge of them. The cache is bounded
// in size and time, it never stores a stream URL, and it never stores a failure
// that could be temporary: only a result, or a failure the error taxonomy
// attributes to the requested item itself, is replayed.

// Default reuse bounds. Results are reused for minutes, not hours: the audio is
// always fetched by a fresh download process, so a reused extraction only
// saves the metadata round trip and can never hand out an expired stream URL.
const (
	DefaultSearchTTL   = 10 * time.Minute
	DefaultExtractTTL  = 10 * time.Minute
	DefaultNegativeTTL = 15 * time.Minute
	DefaultMaxEntries  = 2048
)

// QueryCacheOptions bounds the reuse of metadata query outcomes. Zero values
// select the defaults.
type QueryCacheOptions struct {
	// SearchTTL bounds how long a non-empty flat search or listing is reused.
	SearchTTL time.Duration
	// ExtractTTL bounds how long a successful single-item extraction is reused.
	ExtractTTL time.Duration
	// NegativeTTL bounds how long an item-scoped extraction failure, such as an
	// unavailable or DRM protected item, is replayed.
	NegativeTTL time.Duration
	// MaxEntries bounds the number of stored outcomes.
	MaxEntries int
}

func (o QueryCacheOptions) withDefaults() QueryCacheOptions {
	if o.SearchTTL <= 0 {
		o.SearchTTL = DefaultSearchTTL
	}
	if o.ExtractTTL <= 0 {
		o.ExtractTTL = DefaultExtractTTL
	}
	if o.NegativeTTL <= 0 {
		o.NegativeTTL = DefaultNegativeTTL
	}
	if o.MaxEntries <= 0 {
		o.MaxEntries = DefaultMaxEntries
	}
	return o
}

// queryKind separates listings from single-item extractions. They are reused
// under different rules.
type queryKind string

const (
	querySearch  queryKind = "search"
	queryExtract queryKind = "extract"
)

func queryKindOf(extra []string) queryKind {
	for _, arg := range extra {
		if arg == "--flat-playlist" {
			return querySearch
		}
	}
	return queryExtract
}

// queryOutcome reports how a query was answered.
type queryOutcome string

const (
	outcomeProcess queryOutcome = "process"
	outcomeCached  queryOutcome = "cache_hit"
	outcomeShared  queryOutcome = "shared"
)

type queryCache struct {
	mu       sync.Mutex
	opts     QueryCacheOptions
	entries  map[string]*list.Element
	lru      *list.List // front is the most recently used entry
	inflight map[string]*queryCall
	now      func() time.Time
}

type cacheEntry struct {
	key     string
	infos   []Info
	err     error
	expires time.Time
}

// queryCall is one running process that identical concurrent queries wait for.
type queryCall struct {
	done  chan struct{}
	infos []Info
	err   error
	// abandoned marks a call whose leader stopped for a reason of its own -
	// its context ended. Its outcome says nothing about the item, so waiting
	// followers must run the query themselves instead of adopting it.
	abandoned bool
}

func newQueryCache(opts QueryCacheOptions) *queryCache {
	return &queryCache{
		opts:     opts.withDefaults(),
		entries:  make(map[string]*list.Element),
		lru:      list.New(),
		inflight: make(map[string]*queryCall),
		now:      time.Now,
	}
}

// queryCacheKey identifies an invocation. The argument vector contains the
// cookie file path, so the key is hashed: it is only ever compared, and nothing
// derived from a credential location is kept in readable form.
func queryCacheKey(binary string, args []string) string {
	sum := sha256.Sum256([]byte(binary + "\x00" + strings.Join(args, "\x00")))
	return hex.EncodeToString(sum[:])
}

// do answers a query from a stored outcome, from an identical running query,
// or by running it.
func (q *queryCache) do(ctx context.Context, key string, kind queryKind, run func() ([]Info, error)) ([]Info, error, queryOutcome) {
	for {
		q.mu.Lock()
		if el, ok := q.entries[key]; ok {
			entry := el.Value.(*cacheEntry)
			if q.now().Before(entry.expires) {
				q.lru.MoveToFront(el)
				infos, err := cloneInfos(entry.infos), entry.err
				q.mu.Unlock()
				return infos, err, outcomeCached
			}
			q.removeLocked(el)
		}

		if call, ok := q.inflight[key]; ok {
			q.mu.Unlock()
			select {
			case <-call.done:
				if call.abandoned {
					if ctx.Err() != nil {
						return nil, contextError(ctx), outcomeShared
					}
					continue
				}
				return cloneInfos(call.infos), call.err, outcomeShared
			case <-ctx.Done():
				return nil, contextError(ctx), outcomeShared
			}
		}

		call := &queryCall{done: make(chan struct{})}
		q.inflight[key] = call
		q.mu.Unlock()

		infos, err := q.lead(ctx, key, kind, call, run)
		return infos, err, outcomeProcess
	}
}

// lead runs the query for everybody waiting on call. The deferred cleanup also
// covers a panic, so no follower can be left waiting forever.
func (q *queryCache) lead(ctx context.Context, key string, kind queryKind, call *queryCall, run func() ([]Info, error)) ([]Info, error) {
	var (
		infos    []Info
		err      error
		finished bool
	)
	defer func() {
		q.mu.Lock()
		if !finished || ctx.Err() != nil {
			call.abandoned = true
		} else {
			// Followers and the store get copies of their own; the leader
			// returns yet another one, so nobody shares mutable state.
			call.infos, call.err = cloneInfos(infos), err
			if ttl := q.ttlFor(kind, infos, err); ttl > 0 {
				q.storeLocked(key, infos, err, ttl)
			}
		}
		delete(q.inflight, key)
		q.mu.Unlock()
		close(call.done)
	}()

	infos, err = run()
	if err == nil && kind == queryExtract {
		infos = sanitizeExtraction(infos)
	}
	finished = true
	return cloneInfos(infos), err
}

// ttlFor decides whether an outcome may be reused and for how long. Empty
// results and every failure outside the item's own scope - rate limits,
// network errors, timeouts, session and tool problems - are never stored.
func (q *queryCache) ttlFor(kind queryKind, infos []Info, err error) time.Duration {
	if err != nil {
		if kind == queryExtract && apperr.CodeOf(err) == apperr.CodeTrackNotFound {
			return q.opts.NegativeTTL
		}
		return 0
	}
	if len(infos) == 0 {
		return 0
	}
	if kind == querySearch {
		return q.opts.SearchTTL
	}
	return q.opts.ExtractTTL
}

func (q *queryCache) storeLocked(key string, infos []Info, err error, ttl time.Duration) {
	now := q.now()
	if el, ok := q.entries[key]; ok {
		q.removeLocked(el)
	}
	if len(q.entries) >= q.opts.MaxEntries {
		q.pruneExpiredLocked(now)
	}
	for len(q.entries) >= q.opts.MaxEntries {
		q.removeLocked(q.lru.Back())
	}
	entry := &cacheEntry{key: key, infos: cloneInfos(infos), err: err, expires: now.Add(ttl)}
	q.entries[key] = q.lru.PushFront(entry)
}

func (q *queryCache) pruneExpiredLocked(now time.Time) {
	for el := q.lru.Back(); el != nil; {
		prev := el.Prev()
		if !now.Before(el.Value.(*cacheEntry).expires) {
			q.removeLocked(el)
		}
		el = prev
	}
}

func (q *queryCache) removeLocked(el *list.Element) {
	if el == nil {
		return
	}
	delete(q.entries, el.Value.(*cacheEntry).key)
	q.lru.Remove(el)
}

func (q *queryCache) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.entries)
}

// sanitizeExtraction drops the top-level url of a full extraction. For a
// single-item extraction it can be the stream URL of the selected format,
// which expires and must never outlive the process that produced it. Page
// addresses are kept in webpage_url, and formats carry no URL at all.
func sanitizeExtraction(infos []Info) []Info {
	out := cloneInfos(infos)
	for i := range out {
		out[i].URL = ""
	}
	return out
}

// cloneInfos copies the result so that no caller can alter a stored outcome
// or another caller's copy.
func cloneInfos(infos []Info) []Info {
	if infos == nil {
		return nil
	}
	out := make([]Info, len(infos))
	copy(out, infos)
	for i := range out {
		if out[i].Formats != nil {
			out[i].Formats = append([]Format(nil), out[i].Formats...)
		}
	}
	return out
}

// contextError mirrors the error run reports when a query's context ends.
func contextError(ctx context.Context) error {
	return apperr.Wrap(apperr.CodeProviderUnavailable, "The yt-dlp query timed out.", ctx.Err())
}
