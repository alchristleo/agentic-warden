package handler

import (
	"container/list"
	"log/slog"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	limitBurst       = 10
	limitRefill      = 6 * time.Second // one failure token back per interval
	limitMaxKeys     = 10000
	limitBlockedBody = "too many failed attempts"
)

// Limiter slows repeated failed authentication per client address. Each
// client key holds a token bucket: a counted failure takes one token and an
// empty bucket gets 429 before the wrapped handler runs.
type Limiter struct {
	trusted []netip.Prefix
	now     func() time.Time
	log     *slog.Logger

	mu      sync.Mutex
	buckets map[netip.Prefix]*list.Element // value: *bucket
	lru     *list.List                     // front = most recently used
	warned  sync.Once
}

type bucket struct {
	key    netip.Prefix
	tokens float64
	last   time.Time
}

// NewLimiter builds a limiter. X-Forwarded-For is read only when the
// connection's address is inside trusted.
func NewLimiter(trusted []netip.Prefix, now func() time.Time, log *slog.Logger) *Limiter {
	return &Limiter{trusted: trusted, now: now, log: log, buckets: map[netip.Prefix]*list.Element{}, lru: list.New()}
}

// Wrap guards next. /healthz and /readyz are never blocked or counted.
func (l *Limiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		key := l.key(r)
		wait, taken := l.reserve(key)
		if wait > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
			writeError(w, http.StatusTooManyRequests, limitBlockedBody)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if !countedFailure(r, rec.status) {
			l.refund(key, taken)
		}
	})
}

// countedFailure reports whether a response is a failed credential guess.
// Machine credential 401s do not count: revoked machines poll forever.
func countedFailure(r *http.Request, status int) bool {
	switch status {
	case http.StatusUnauthorized:
		return !(r.Method == http.MethodGet && r.URL.Path == "/v1/bundle")
	case http.StatusNotFound, http.StatusConflict:
		return r.Method == http.MethodPost && r.URL.Path == "/v1/machines/enroll"
	}
	return false
}

// refilled returns b's tokens as of now, capped at the burst.
func (l *Limiter) refilled(b *bucket, now time.Time) float64 {
	t := b.tokens + float64(now.Sub(b.last))/float64(limitRefill)
	return math.Min(t, limitBurst)
}

// reserve takes one token for key before the request runs. It returns how
// long until key may try again, or 0 when a token was taken, and the bucket
// the token came from. Check and take happen under one lock, so concurrent
// requests cannot overshoot the burst.
func (l *Limiter) reserve(key netip.Prefix) (time.Duration, *list.Element) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	el, ok := l.buckets[key]
	if ok {
		l.lru.MoveToFront(el)
	} else {
		if len(l.buckets) >= limitMaxKeys {
			// Drop the least recently used key. Its bucket restarts full.
			oldest := l.lru.Back()
			l.lru.Remove(oldest)
			delete(l.buckets, oldest.Value.(*bucket).key)
		}
		el = l.lru.PushFront(&bucket{key: key, tokens: limitBurst, last: now})
		l.buckets[key] = el
	}
	b := el.Value.(*bucket)
	b.tokens = l.refilled(b, now)
	b.last = now
	if b.tokens < 1 {
		return time.Duration((1 - b.tokens) * float64(limitRefill)), el
	}
	b.tokens--
	return 0, el
}

// refund gives back a token reserve took from el when the request was not a
// counted failure. If el was evicted meanwhile, a new bucket for the same key
// is not credited. A bucket that is full again is dropped, so the map holds only
// keys that failed recently.
func (l *Limiter) refund(key netip.Prefix, taken *list.Element) {
	l.mu.Lock()
	defer l.mu.Unlock()
	el, ok := l.buckets[key]
	if !ok || el != taken {
		return
	}
	b := el.Value.(*bucket)
	b.tokens = l.refilled(b, l.now()) + 1
	b.last = l.now()
	if b.tokens >= limitBurst {
		l.lru.Remove(el)
		delete(l.buckets, key)
	}
}

// key names the client: the connection's address, or behind a trusted proxy
// the rightmost X-Forwarded-For entry that is not itself a trusted proxy.
// IPv6 addresses collapse to their /64.
func (l *Limiter) key(r *http.Request) netip.Prefix {
	remote := parseClientAddr(r.RemoteAddr)
	if len(l.trusted) == 0 {
		if len(r.Header.Values("X-Forwarded-For")) > 0 {
			l.warned.Do(func() {
				l.log.Warn("X-Forwarded-For seen but AWD_TRUSTED_PROXIES is unset: all clients behind the proxy share one failed-auth bucket")
			})
		}
		return bucketKey(remote)
	}
	if !l.isTrusted(remote) {
		return bucketKey(remote)
	}
	entries := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(entries) - 1; i >= 0; i-- {
		addr := parseClientAddr(strings.TrimSpace(entries[i]))
		if !addr.IsValid() {
			break
		}
		if !l.isTrusted(addr) {
			return bucketKey(addr)
		}
	}
	return bucketKey(remote)
}

func (l *Limiter) isTrusted(a netip.Addr) bool {
	for _, p := range l.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// parseClientAddr accepts ip, ip:port and [ipv6]:port. It returns the zero
// Addr when s is none of these.
func parseClientAddr(s string) netip.Addr {
	if a, err := netip.ParseAddr(s); err == nil {
		return a.Unmap()
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap()
	}
	return netip.Addr{}
}

func bucketKey(a netip.Addr) netip.Prefix {
	if !a.IsValid() {
		return netip.Prefix{}
	}
	if a.Is4() {
		return netip.PrefixFrom(a, 32)
	}
	p, _ := a.Prefix(64)
	return p
}
