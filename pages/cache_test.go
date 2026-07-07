package pages

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// serveOverPipe exercises the real connection-level ServeHTTP (the one
// the proxy calls), so the test sees the cache-policy headers applied
// there — not just the inner mux's. It writes the response to one end
// of a net.Pipe and parses it back off the other.
func serveOverPipe(t *testing.T, h *Handler, req *http.Request) (*http.Response, []byte) {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(server, req)
		server.Close()
		close(done)
	}()

	resp, err := http.ReadResponse(bufio.NewReader(client), req)
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	resp.Body.Close()
	client.Close()
	<-done
	return resp, body
}

func newRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodGet, target, nil)
}

// Static assets must be cacheable but revalidated against the build
// version, so a reselected (discarded) tab gets a cheap 304 instead of
// re-downloading the render-blocking CSS/JS/font bundle. The
// stale-while-revalidate window lets the browser paint from cache
// immediately and revalidate in the background instead of blocking
// first paint on the round trip.
func TestStaticAssetsRevalidateByVersion(t *testing.T) {
	h := New([]PageInfo{{Name: "dev", Page: &PageConfig{}}}, nil, nil)

	resp, body := serveOverPipe(t, h, newRequest(t, "http://pages.subspace.pub/dev/static/style.css"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "max-age=0, stale-while-revalidate=86400" {
		t.Errorf("Cache-Control = %q, want max-age=0, stale-while-revalidate=86400", cc)
	}
	wantETag := `"` + Version + `"`
	if et := resp.Header.Get("ETag"); et != wantETag {
		t.Errorf("ETag = %q, want %q", et, wantETag)
	}
	if len(body) == 0 {
		t.Errorf("body is empty, want stylesheet bytes")
	}

	// A matching If-None-Match revalidates to 304 with no body.
	req := newRequest(t, "http://pages.subspace.pub/dev/static/style.css")
	req.Header.Set("If-None-Match", wantETag)
	resp304, body304 := serveOverPipe(t, h, req)
	if resp304.StatusCode != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", resp304.StatusCode)
	}
	if len(body304) != 0 {
		t.Errorf("304 body = %q, want empty", body304)
	}
}

// The dashboard HTML and config-derived APIs revalidate against a hash
// of their body (i.e. the pages/config they render from), so an edit
// invalidates them while an unchanged config returns 304.
func TestDynamicResponsesRevalidateByContentHash(t *testing.T) {
	h := New([]PageInfo{{Name: "dev", Page: &PageConfig{Title: "Dev"}}}, nil, nil)

	for _, target := range []string{
		"http://pages.subspace.pub/dev/",
		"http://pages.subspace.pub/dev/api/links",
		"http://pages.subspace.pub/dev/api/nav",
		"http://pages.subspace.pub/dev/api/all-links",
		"http://pages.subspace.pub/dev/api/search",
	} {
		resp, body := serveOverPipe(t, h, newRequest(t, target))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", target, resp.StatusCode)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "max-age=0, stale-while-revalidate=86400" {
			t.Errorf("%s: Cache-Control = %q, want max-age=0, stale-while-revalidate=86400", target, cc)
		}
		etag := resp.Header.Get("ETag")
		if etag == "" || etag == `"`+Version+`"` {
			t.Errorf("%s: ETag = %q, want a content hash distinct from the version", target, etag)
		}

		req := newRequest(t, target)
		req.Header.Set("If-None-Match", etag)
		resp304, body304 := serveOverPipe(t, h, req)
		if resp304.StatusCode != http.StatusNotModified {
			t.Errorf("%s: conditional status = %d, want 304", target, resp304.StatusCode)
		}
		if len(body304) != 0 {
			t.Errorf("%s: 304 body non-empty", target)
		}
		_ = body
	}
}

// A different config must produce a different content-hash ETag, so the
// browser refetches the changed page instead of serving a stale 304.
func TestContentHashTracksConfig(t *testing.T) {
	h1 := New([]PageInfo{{Name: "dev", Page: &PageConfig{Title: "One"}}}, nil, nil)
	h2 := New([]PageInfo{{Name: "dev", Page: &PageConfig{Title: "Two"}}}, nil, nil)

	target := "http://pages.subspace.pub/dev/api/links"
	r1, _ := serveOverPipe(t, h1, newRequest(t, target))
	r2, _ := serveOverPipe(t, h2, newRequest(t, target))

	if e1, e2 := r1.Header.Get("ETag"), r2.Header.Get("ETag"); e1 == "" || e1 == e2 {
		t.Errorf("ETags should differ for different config: %q vs %q", e1, e2)
	}
}

// Live metrics, the config-version poll, redirects and errors must stay
// uncached so they're always fresh.
func TestVolatileResponsesAreNoStore(t *testing.T) {
	h := New([]PageInfo{{Name: "dev", Page: &PageConfig{}}}, nil, nil)

	resp, _ := serveOverPipe(t, h, newRequest(t, "http://pages.subspace.pub/dev/api/config-errors"))
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("config-errors Cache-Control = %q, want no-store", cc)
	}
	if et := resp.Header.Get("ETag"); et != "" {
		t.Errorf("config-errors ETag = %q, want none", et)
	}

	// Root redirect to the first page must not be cached.
	redir, _ := serveOverPipe(t, h, newRequest(t, "http://pages.subspace.pub/"))
	if redir.StatusCode != http.StatusFound {
		t.Fatalf("root status = %d, want 302", redir.StatusCode)
	}
	if cc := redir.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("redirect Cache-Control = %q, want no-store", cc)
	}
}

// Every internal response must carry a Content-Length so a keep-alive
// connection knows where the body ends; an EOF-framed body would force
// closing the connection after every response.
func TestResponsesAreLengthFramed(t *testing.T) {
	h := New([]PageInfo{{Name: "dev", Page: &PageConfig{Title: "Dev"}}}, nil, nil)

	for _, target := range []string{
		"http://pages.subspace.pub/dev/",
		"http://pages.subspace.pub/dev/api/links",
		"http://pages.subspace.pub/dev/api/config-errors",
	} {
		resp, body := serveOverPipe(t, h, newRequest(t, target))
		if resp.ContentLength < 0 {
			t.Errorf("%s: response is EOF-framed (no Content-Length)", target)
		} else if resp.ContentLength != int64(len(body)) {
			t.Errorf("%s: Content-Length = %d, body = %d bytes", target, resp.ContentLength, len(body))
		}
	}
}

// The favicon endpoint deliberately sets a long-lived Cache-Control;
// the connection-level handler must not clobber it back to no-store.
func TestFaviconCacheHeaderSurvives(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/favicon.ico" {
			atomic.AddInt32(&hits, 1)
			w.Header().Set("Content-Type", "image/x-icon")
			w.Write([]byte("FAKEICONBYTES"))
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	h := New([]PageInfo{{Name: "dev", Page: &PageConfig{}}}, nil, nil)
	h.SetSearchEngines(map[string]SearchEngineDef{
		"test": {Name: "test", URL: upstream.URL + "/search?q={query}"},
	}, "")
	host := strings.TrimPrefix(upstream.URL, "http://")

	resp, body := serveOverPipe(t, h,
		newRequest(t, "http://pages.subspace.pub/dev/api/favicon?host="+host))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("favicon status = %d, want 200 (body %q)", resp.StatusCode, body)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "max-age") {
		t.Errorf("favicon Cache-Control = %q, want a max-age directive (not clobbered)", cc)
	}
}
