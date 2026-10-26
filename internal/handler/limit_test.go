package handler

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// statusInner answers the status the test sets and counts its calls.
type statusInner struct {
	status int
	calls  int
}

func (s *statusInner) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.calls++
	w.WriteHeader(s.status)
}

func limiterFor(trusted ...string) (*Limiter, *fakeClock, *bytes.Buffer) {
	var prefixes []netip.Prefix
	for _, p := range trusted {
		prefixes = append(prefixes, netip.MustParsePrefix(p))
	}
	clock := &fakeClock{t: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}
	logs := &bytes.Buffer{}
	return NewLimiter(prefixes, clock.now, slog.New(slog.NewTextHandler(logs, nil))), clock, logs
}

func do(h http.Handler, method, path, remote string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = remote
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Add(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLimiterBlocksAfterTenFailures(t *testing.T) {
	l, _, _ := limiterFor()
	inner := &statusInner{status: http.StatusUnauthorized}
	h := l.Wrap(inner)
	for i := 0; i < 10; i++ {
		if got := do(h, "GET", "/v1/policy", "192.0.2.1:1000").Code; got != 401 {
			t.Fatalf("request %d: %d", i, got)
		}
	}
	inner.status = http.StatusOK
	before := inner.calls
	rec := do(h, "GET", "/v1/policy", "192.0.2.1:1000")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("11th request: %d", rec.Code)
	}
	if inner.calls != before {
		t.Fatal("inner handler ran for a blocked key")
	}
	if rec.Header().Get("Retry-After") != "6" {
		t.Fatalf("Retry-After = %q", rec.Header().Get("Retry-After"))
	}
	if strings.TrimSpace(rec.Body.String()) != `{"error":"too many failed attempts"}` {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestLimiterRefills(t *testing.T) {
	l, clock, _ := limiterFor()
	inner := &statusInner{status: http.StatusUnauthorized}
	h := l.Wrap(inner)
	for i := 0; i < 10; i++ {
		do(h, "GET", "/v1/policy", "192.0.2.1:1")
	}
	clock.advance(5 * time.Second)
	if got := do(h, "GET", "/v1/policy", "192.0.2.1:1").Code; got != 429 {
		t.Fatalf("after 5s: %d", got)
	}
	clock.advance(1 * time.Second)
	if got := do(h, "GET", "/v1/policy", "192.0.2.1:1").Code; got != 401 {
		t.Fatalf("after 6s: %d", got)
	}
	if got := do(h, "GET", "/v1/policy", "192.0.2.1:1").Code; got != 429 {
		t.Fatalf("second attempt after 6s: %d", got)
	}
}

func TestLimiterHealthExempt(t *testing.T) {
	l, _, _ := limiterFor()
	inner := &statusInner{status: http.StatusUnauthorized}
	h := l.Wrap(inner)
	for i := 0; i < 10; i++ {
		do(h, "GET", "/v1/policy", "192.0.2.1:1")
	}
	inner.status = http.StatusOK
	for _, p := range []string{"/healthz", "/readyz"} {
		if got := do(h, "GET", p, "192.0.2.1:1").Code; got != 200 {
			t.Fatalf("%s: %d", p, got)
		}
	}
}

func TestLimiterOtherIPUnaffected(t *testing.T) {
	l, _, _ := limiterFor()
	inner := &statusInner{status: http.StatusUnauthorized}
	h := l.Wrap(inner)
	for i := 0; i < 10; i++ {
		do(h, "GET", "/v1/policy", "192.0.2.1:1")
	}
	inner.status = http.StatusOK
	if got := do(h, "GET", "/v1/policy", "192.0.2.2:1").Code; got != 200 {
		t.Fatalf("other IP: %d", got)
	}
}

// exhaust sends 10 requests the limiter must count, then reports whether the
// 11th is blocked.
func exhaust(method, path string, status int) bool {
	l, _, _ := limiterFor()
	h := l.Wrap(&statusInner{status: status})
	for i := 0; i < 10; i++ {
		do(h, method, path, "192.0.2.1:1")
	}
	return do(h, method, path, "192.0.2.1:1").Code == http.StatusTooManyRequests
}

func TestLimiterCountsAdmin401(t *testing.T) {
	if !exhaust("GET", "/v1/policy", 401) {
		t.Fatal("admin 401 not counted")
	}
}

func TestLimiterCountsSCIM401(t *testing.T) {
	if !exhaust("GET", "/scim/v2/Users", 401) {
		t.Fatal("SCIM 401 not counted")
	}
}

func TestLimiterCountsEnroll404And409(t *testing.T) {
	for _, status := range []int{404, 409} {
		if !exhaust("POST", "/v1/machines/enroll", status) {
			t.Fatalf("enroll %d not counted", status)
		}
	}
	if exhaust("POST", "/v1/machines/enroll", 200) {
		t.Fatal("enroll 200 counted")
	}
	if exhaust("GET", "/v1/policy", 404) {
		t.Fatal("404 outside enroll counted")
	}
}

func TestLimiterSkipsBundle401(t *testing.T) {
	if exhaust("GET", "/v1/bundle", 401) {
		t.Fatal("machine 401 counted")
	}
}

func TestLimiterKeyUntrustedXFFIgnored(t *testing.T) {
	l, _, _ := limiterFor()
	h := l.Wrap(&statusInner{status: 401})
	for i := 0; i < 10; i++ {
		do(h, "GET", "/v1/policy", "192.0.2.1:1", "X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i))
	}
	if got := do(h, "GET", "/v1/policy", "192.0.2.1:1", "X-Forwarded-For", "198.51.100.99").Code; got != 429 {
		t.Fatalf("spoofed XFF dodged the limit: %d", got)
	}
}

func TestLimiterKeyUntrustedPeerXFFIgnoredWhenProxiesSet(t *testing.T) {
	l, _, _ := limiterFor("10.0.0.0/8")
	h := l.Wrap(&statusInner{status: 401})
	for i := 0; i < 10; i++ {
		do(h, "GET", "/v1/policy", "192.0.2.1:1", "X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i))
	}
	if got := do(h, "GET", "/v1/policy", "192.0.2.1:1", "X-Forwarded-For", "198.51.100.99").Code; got != 429 {
		t.Fatalf("XFF from an untrusted peer dodged the limit: %d", got)
	}
}

func TestLimiterKeyTrustedRightmostUntrusted(t *testing.T) {
	l, _, _ := limiterFor("10.0.0.0/8")
	h := l.Wrap(&statusInner{status: 401})
	for i := 0; i < 10; i++ {
		do(h, "GET", "/v1/policy", "10.0.0.5:1", "X-Forwarded-For", "203.0.113.7, 10.0.0.9")
	}
	if got := do(h, "GET", "/v1/policy", "10.0.0.5:1", "X-Forwarded-For", "203.0.113.7").Code; got != 429 {
		t.Fatalf("same client: %d", got)
	}
	if got := do(h, "GET", "/v1/policy", "10.0.0.5:1", "X-Forwarded-For", "203.0.113.8").Code; got != 401 {
		t.Fatalf("other client behind proxy: %d", got)
	}
}

func TestLimiterKeyTwoHeaderLines(t *testing.T) {
	l, _, _ := limiterFor("10.0.0.0/8")
	h := l.Wrap(&statusInner{status: 401})
	for i := 0; i < 10; i++ {
		do(h, "GET", "/v1/policy", "10.0.0.5:1", "X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i), "X-Forwarded-For", "203.0.113.7, 10.0.0.9")
	}
	if got := do(h, "GET", "/v1/policy", "10.0.0.5:1", "X-Forwarded-For", "203.0.113.7").Code; got != 429 {
		t.Fatalf("two lines not joined: %d", got)
	}
}

func TestLimiterKeyIPPortEntries(t *testing.T) {
	l, _, _ := limiterFor("10.0.0.0/8")
	h := l.Wrap(&statusInner{status: 401})
	for i := 0; i < 10; i++ {
		do(h, "GET", "/v1/policy", "10.0.0.5:1", "X-Forwarded-For", "203.0.113.7:5555, 10.0.0.9:80")
	}
	if got := do(h, "GET", "/v1/policy", "10.0.0.5:1", "X-Forwarded-For", "203.0.113.7").Code; got != 429 {
		t.Fatalf("ip:port not parsed: %d", got)
	}
	l, _, _ = limiterFor("10.0.0.0/8")
	h = l.Wrap(&statusInner{status: 401})
	for i := 0; i < 10; i++ {
		do(h, "GET", "/v1/policy", "10.0.0.5:1", "X-Forwarded-For", "[2001:db8:1:2::7]:443")
	}
	if got := do(h, "GET", "/v1/policy", "10.0.0.5:1", "X-Forwarded-For", "2001:db8:1:2::8").Code; got != 429 {
		t.Fatalf("[v6]:port not parsed: %d", got)
	}
}

func TestLimiterKeyForgedLeftEntryIgnored(t *testing.T) {
	l, _, _ := limiterFor("10.0.0.0/8")
	h := l.Wrap(&statusInner{status: 401})
	// The attacker (203.0.113.7) writes a fresh left entry each time. The
	// proxy appends the attacker's real address on the right.
	for i := 0; i < 10; i++ {
		do(h, "GET", "/v1/policy", "10.0.0.5:1", "X-Forwarded-For", fmt.Sprintf("10.0.0.%d, 192.0.2.%d, 203.0.113.7", i+20, i+20))
	}
	if got := do(h, "GET", "/v1/policy", "10.0.0.5:1", "X-Forwarded-For", "10.9.9.9, 192.0.2.99, 203.0.113.7").Code; got != 429 {
		t.Fatalf("forged left entry dodged the limit: %d", got)
	}
}

func TestLimiterKeyIPv6SharesSlash64(t *testing.T) {
	l, _, _ := limiterFor()
	h := l.Wrap(&statusInner{status: 401})
	for i := 0; i < 10; i++ {
		do(h, "GET", "/v1/policy", fmt.Sprintf("[2001:db8:1:2::%x]:1", i+1))
	}
	if got := do(h, "GET", "/v1/policy", "[2001:db8:1:2:ffff::1]:1").Code; got != 429 {
		t.Fatalf("same /64: %d", got)
	}
	if got := do(h, "GET", "/v1/policy", "[2001:db8:1:3::1]:1").Code; got != 401 {
		t.Fatalf("other /64: %d", got)
	}
}

func TestLimiterKeyWarnsOnceWithoutTrustedProxies(t *testing.T) {
	l, _, logs := limiterFor()
	h := l.Wrap(&statusInner{status: 200})
	do(h, "GET", "/healthz", "192.0.2.1:1")
	if logs.Len() != 0 {
		t.Fatalf("warned without XFF: %s", logs)
	}
	do(h, "GET", "/v1/policy", "192.0.2.1:1", "X-Forwarded-For", "198.51.100.1")
	do(h, "GET", "/v1/policy", "192.0.2.1:1", "X-Forwarded-For", "198.51.100.2")
	if n := strings.Count(logs.String(), "level=WARN"); n != 1 {
		t.Fatalf("warnings = %d: %s", n, logs)
	}
	l, _, logs = limiterFor("10.0.0.0/8")
	do(l.Wrap(&statusInner{status: 200}), "GET", "/v1/policy", "10.0.0.1:1", "X-Forwarded-For", "198.51.100.1")
	if logs.Len() != 0 {
		t.Fatalf("warned with trusted proxies: %s", logs)
	}
}

func TestLimiterBounded(t *testing.T) {
	l, _, _ := limiterFor()
	h := l.Wrap(&statusInner{status: 401})
	for i := 0; i < 20000; i++ {
		do(h, "GET", "/v1/policy", fmt.Sprintf("10.%d.%d.%d:1", i>>16&255, i>>8&255, i&255))
	}
	l.mu.Lock()
	n := len(l.buckets)
	l.mu.Unlock()
	if n > 10000 {
		t.Fatalf("buckets = %d", n)
	}
	if n == 0 {
		t.Fatal("limiter forgot everything")
	}
}

func v4Key(i int) netip.Prefix {
	return netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}), 32)
}

func TestLimiterEvictsLeastRecentlyUsed(t *testing.T) {
	l, _, _ := limiterFor()
	h := l.Wrap(&statusInner{status: 401})
	addr := func(i int) string { return v4Key(i).Addr().String() + ":1" }
	for i := 0; i < limitMaxKeys; i++ {
		do(h, "GET", "/v1/policy", addr(i))
	}
	for i := 0; i < 100; i++ { // touch the 100 oldest
		do(h, "GET", "/v1/policy", addr(i))
	}
	do(h, "GET", "/v1/policy", addr(limitMaxKeys)) // one more key at the cap
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buckets) != limitMaxKeys {
		t.Fatalf("buckets = %d", len(l.buckets))
	}
	if _, ok := l.buckets[v4Key(100)]; ok {
		t.Fatal("least recently used key survived")
	}
	for _, i := range []int{0, 99, 101, limitMaxKeys - 1, limitMaxKeys} {
		if _, ok := l.buckets[v4Key(i)]; !ok {
			t.Fatalf("key %d was dropped", i)
		}
	}
}

// countInner is safe for concurrent use.
type countInner struct {
	status int
	calls  atomic.Int64
}

func (c *countInner) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	c.calls.Add(1)
	w.WriteHeader(c.status)
}

func TestLimiterConcurrentOneKeyCannotOvershoot(t *testing.T) {
	l, _, _ := limiterFor()
	inner := &countInner{status: 401}
	h := l.Wrap(inner)
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			do(h, "GET", "/v1/policy", "192.0.2.1:1")
		}()
	}
	wg.Wait()
	if got := inner.calls.Load(); got != 10 {
		t.Fatalf("inner calls = %d, want 10", got)
	}
}

func TestLimiterConcurrentDistinctKeys(t *testing.T) {
	l, _, _ := limiterFor()
	inner := &countInner{status: 401}
	h := l.Wrap(inner)
	var wg sync.WaitGroup
	for k := 0; k < 50; k++ {
		for j := 0; j < 15; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				do(h, "GET", "/v1/policy", fmt.Sprintf("192.0.2.%d:1", k+1))
			}()
		}
	}
	wg.Wait()
	if got := inner.calls.Load(); got != 500 {
		t.Fatalf("inner calls = %d, want 500", got)
	}
}

func TestLimiterRefundsUncountedRequests(t *testing.T) {
	l, _, _ := limiterFor()
	inner := &statusInner{status: 200}
	h := l.Wrap(inner)
	for i := 0; i < 50; i++ {
		do(h, "GET", "/v1/policy", "192.0.2.1:1")
	}
	if inner.calls != 50 {
		t.Fatalf("inner calls = %d", inner.calls)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buckets) != 0 || l.lru.Len() != 0 {
		t.Fatalf("buckets = %d after only successes", len(l.buckets))
	}
}

func TestLimiterBlockedRequestTakesNoToken(t *testing.T) {
	l, clock, _ := limiterFor()
	inner := &statusInner{status: 401}
	h := l.Wrap(inner)
	for i := 0; i < 10; i++ {
		do(h, "GET", "/v1/policy", "192.0.2.1:1")
	}
	clock.advance(3 * time.Second)
	for i := 0; i < 5; i++ {
		if got := do(h, "GET", "/v1/policy", "192.0.2.1:1").Code; got != 429 {
			t.Fatalf("blocked request %d: %d", i, got)
		}
	}
	clock.advance(3 * time.Second)
	if got := do(h, "GET", "/v1/policy", "192.0.2.1:1").Code; got != 401 {
		t.Fatalf("after one refill interval: %d", got)
	}
	if got := do(h, "GET", "/v1/policy", "192.0.2.1:1").Code; got != 429 {
		t.Fatalf("second request after one interval: %d", got)
	}
}

func TestLimiterRefundNeverExceedsBurst(t *testing.T) {
	l, clock, _ := limiterFor()
	inner := &statusInner{status: 401}
	h := l.Wrap(inner)
	do(h, "GET", "/v1/policy", "192.0.2.1:1") // 9 tokens
	clock.advance(limitRefill)                // back to 10 by time alone
	inner.status = 200
	do(h, "GET", "/v1/policy", "192.0.2.1:1") // reserve to 9, refund to 10
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, el := range l.buckets {
		if tk := el.Value.(*bucket).tokens; tk > limitBurst {
			t.Fatalf("tokens = %v above burst", tk)
		}
	}
	if len(l.buckets) != 0 {
		t.Fatalf("full bucket kept: %d", len(l.buckets))
	}
}

func TestLimiterRefundSkipsRecreatedBucket(t *testing.T) {
	l, _, _ := limiterFor()
	key := netip.MustParsePrefix("192.0.2.1/32")
	h := l.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// While this request runs, other clients push its key out of the
		// cache and the key comes back with a fresh bucket.
		for i := 0; i < limitMaxKeys; i++ {
			l.reserve(v4Key(i))
		}
		l.reserve(key)
		w.WriteHeader(http.StatusOK)
	}))
	do(h, "GET", "/v1/policy", "192.0.2.1:1")
	l.mu.Lock()
	defer l.mu.Unlock()
	el, ok := l.buckets[key]
	if !ok {
		t.Fatal("recreated bucket missing")
	}
	if tk := el.Value.(*bucket).tokens; tk != limitBurst-1 {
		t.Fatalf("tokens = %v, want %d: refund credited the new bucket", tk, limitBurst-1)
	}
}
