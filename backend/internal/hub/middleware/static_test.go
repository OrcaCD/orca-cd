package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"
)

var testModTime = time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)

func newTestServer(files fstest.MapFS, preCompressed preCompressedMap) *FileServerWithCaching {
	return NewFileServerWithCaching(http.FS(files), preCompressed, testModTime)
}

func TestListPreCompressedAssets_EmptyFS(t *testing.T) {
	t.Parallel()

	result, err := listPreCompressedAssets(fstest.MapFS{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestListPreCompressedAssets_DetectsBrotli(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"app.js.br": &fstest.MapFile{Data: []byte("br")},
	}
	result, err := listPreCompressedAssets(testFS)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry, ok := result["/app.js"]
	if !ok {
		t.Fatal("expected /app.js entry in result")
	}
	if !entry.br || entry.gz || entry.zstd {
		t.Errorf("expected br=true gz=false zstd=false, got %+v", entry)
	}
}

func TestListPreCompressedAssets_DetectsGzip(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"styles.css.gz": &fstest.MapFile{Data: []byte("gz")},
	}
	result, err := listPreCompressedAssets(testFS)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry, ok := result["/styles.css"]
	if !ok {
		t.Fatal("expected /styles.css entry in result")
	}
	if entry.br || !entry.gz || entry.zstd {
		t.Errorf("expected br=false gz=true zstd=false, got %+v", entry)
	}
}

func TestListPreCompressedAssets_DetectsZstd(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"vendor.js.zst": &fstest.MapFile{Data: []byte("zst")},
	}
	result, err := listPreCompressedAssets(testFS)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry, ok := result["/vendor.js"]
	if !ok {
		t.Fatal("expected /vendor.js entry in result")
	}
	if entry.br || entry.gz || !entry.zstd {
		t.Errorf("expected br=false gz=false zstd=true, got %+v", entry)
	}
}

func TestListPreCompressedAssets_AllEncodings(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"main.js.br":  &fstest.MapFile{Data: []byte("br")},
		"main.js.gz":  &fstest.MapFile{Data: []byte("gz")},
		"main.js.zst": &fstest.MapFile{Data: []byte("zst")},
	}
	result, err := listPreCompressedAssets(testFS)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry, ok := result["/main.js"]
	if !ok {
		t.Fatal("expected /main.js entry in result")
	}
	if !entry.br || !entry.gz || !entry.zstd {
		t.Errorf("expected all encodings true, got %+v", entry)
	}
}

func TestListPreCompressedAssets_IgnoresUncompressedFiles(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"app.js":    &fstest.MapFile{Data: []byte("js")},
		"index.css": &fstest.MapFile{Data: []byte("css")},
	}
	result, err := listPreCompressedAssets(testFS)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected no entries for uncompressed files, got %v", result)
	}
}

func TestListPreCompressedAssets_NestedDirectory(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"assets/js/app.js.br": &fstest.MapFile{Data: []byte("br")},
		"assets/js/app.js.gz": &fstest.MapFile{Data: []byte("gz")},
	}
	result, err := listPreCompressedAssets(testFS)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry, ok := result["/assets/js/app.js"]
	if !ok {
		t.Fatal("expected /assets/js/app.js entry in result")
	}
	if !entry.br || !entry.gz || entry.zstd {
		t.Errorf("expected br=true gz=true zstd=false, got %+v", entry)
	}
}

func TestListPreCompressedAssets_MultipleFiles(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"app.js.br":     &fstest.MapFile{Data: []byte("br")},
		"styles.css.gz": &fstest.MapFile{Data: []byte("gz")},
	}
	result, err := listPreCompressedAssets(testFS)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	if !result["/app.js"].br {
		t.Error("expected /app.js to have br=true")
	}
	if !result["/styles.css"].gz {
		t.Error("expected /styles.css to have gz=true")
	}
}

func TestSelectEncoding_EmptyAcceptEncoding(t *testing.T) {
	t.Parallel()

	f := &FileServerWithCaching{}
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)

	ext, ce := f.selectEncoding(req, preCompressedEntry{br: true, gz: true, zstd: true})
	if ext != "" || ce != "" {
		t.Errorf("expected empty ext and ce, got ext=%q ce=%q", ext, ce)
	}
}

func TestSelectEncoding_BrotliAccepted(t *testing.T) {
	t.Parallel()

	f := &FileServerWithCaching{}
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "br")

	ext, ce := f.selectEncoding(req, preCompressedEntry{br: true, gz: true})
	if ext != "br" || ce != "br" {
		t.Errorf("expected ext=br ce=br, got ext=%q ce=%q", ext, ce)
	}
}

func TestSelectEncoding_ZstdPreferredOverBrotli(t *testing.T) {
	t.Parallel()

	f := &FileServerWithCaching{}
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "zstd, br, gzip")

	ext, ce := f.selectEncoding(req, preCompressedEntry{br: true, gz: true, zstd: true})
	if ext != extensionZstd || ce != encodingZstd {
		t.Errorf("expected ext=zst ce=zstd, got ext=%q ce=%q", ext, ce)
	}
}

func TestSelectEncoding_BrotliPreferredOverGzip(t *testing.T) {
	t.Parallel()

	f := &FileServerWithCaching{}
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "br, gzip")

	ext, ce := f.selectEncoding(req, preCompressedEntry{br: true, gz: true})
	if ext != "br" || ce != "br" {
		t.Errorf("expected ext=br ce=br, got ext=%q ce=%q", ext, ce)
	}
}

func TestSelectEncoding_GzipFallback(t *testing.T) {
	t.Parallel()

	f := &FileServerWithCaching{}
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	ext, ce := f.selectEncoding(req, preCompressedEntry{gz: true})
	if ext != extensionGzip || ce != encodingGzip {
		t.Errorf("expected ext=gz ce=gzip, got ext=%q ce=%q", ext, ce)
	}
}

func TestSelectEncoding_WildcardSelectsZstd(t *testing.T) {
	t.Parallel()

	f := &FileServerWithCaching{}
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "*")

	ext, ce := f.selectEncoding(req, preCompressedEntry{br: true, gz: true, zstd: true})
	if ext != extensionZstd || ce != encodingZstd {
		t.Errorf("expected ext=zst ce=zstd for wildcard, got ext=%q ce=%q", ext, ce)
	}
}

func TestSelectEncoding_UnavailableEncodingReturnsEmpty(t *testing.T) {
	t.Parallel()

	f := &FileServerWithCaching{}
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "br")

	// Only gzip available; client only accepts brotli.
	ext, ce := f.selectEncoding(req, preCompressedEntry{gz: true})
	if ext != "" || ce != "" {
		t.Errorf("expected empty ext and ce, got ext=%q ce=%q", ext, ce)
	}
}

func TestSelectEncoding_NoneAvailableReturnsEmpty(t *testing.T) {
	t.Parallel()

	f := &FileServerWithCaching{}
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "zstd, br, gzip")

	ext, ce := f.selectEncoding(req, preCompressedEntry{})
	if ext != "" || ce != "" {
		t.Errorf("expected empty ext and ce, got ext=%q ce=%q", ext, ce)
	}
}

func TestSelectEncoding_ZstdOnlyWhenOnlyZstdAvailable(t *testing.T) {
	t.Parallel()

	f := &FileServerWithCaching{}
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip, zstd")

	ext, ce := f.selectEncoding(req, preCompressedEntry{zstd: true})
	if ext != extensionZstd || ce != encodingZstd {
		t.Errorf("expected ext=zst ce=zstd, got ext=%q ce=%q", ext, ce)
	}
}

func TestFileServerWithCaching_NotModified(t *testing.T) {
	t.Parallel()

	fs := newTestServer(fstest.MapFS{}, preCompressedMap{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	// Client claims it has a version from 1 hour after our build time.
	req.Header.Set("If-Modified-Since", testModTime.Add(time.Hour).UTC().Format(http.TimeFormat))

	fs.ServeHTTP(w, req)

	if w.Code != http.StatusNotModified {
		t.Errorf("expected 304, got %d", w.Code)
	}
	if body := w.Body.String(); body != "" {
		t.Errorf("expected empty body for 304, got %q", body)
	}
}

func TestFileServerWithCaching_NotModified_ExactTime(t *testing.T) {
	t.Parallel()

	fs := newTestServer(fstest.MapFS{}, preCompressedMap{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("If-Modified-Since", testModTime.UTC().Format(http.TimeFormat))

	fs.ServeHTTP(w, req)

	if w.Code != http.StatusNotModified {
		t.Errorf("expected 304 for exact build time, got %d", w.Code)
	}
}

func TestFileServerWithCaching_ServesWhenNewerThanCache(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"app.js": &fstest.MapFile{Data: []byte("content"), ModTime: testModTime},
	}
	srv := newTestServer(testFS, preCompressedMap{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("If-Modified-Since", testModTime.Add(-2*time.Hour).UTC().Format(http.TimeFormat))

	srv.ServeHTTP(w, req)

	if w.Code == http.StatusNotModified {
		t.Errorf("expected non-304 for stale cache, got 304")
	}
}

func TestFileServerWithCaching_InvalidIfModifiedSinceServesFile(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"app.js": &fstest.MapFile{Data: []byte("content"), ModTime: testModTime},
	}
	srv := newTestServer(testFS, preCompressedMap{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("If-Modified-Since", "not-a-valid-date")

	srv.ServeHTTP(w, req)

	if w.Code == http.StatusNotModified {
		t.Errorf("expected non-304 for invalid If-Modified-Since, got 304")
	}
}

func TestFileServerWithCaching_SetsCacheControlHeader(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"app.js": &fstest.MapFile{Data: []byte("content"), ModTime: testModTime},
	}
	srv := newTestServer(testFS, preCompressedMap{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)

	srv.ServeHTTP(w, req)

	got := w.Header().Get("Cache-Control")
	if got != defaultCacheHeader {
		t.Errorf("Cache-Control = %q, want %q", got, defaultCacheHeader)
	}
}

func TestFileServerWithCaching_SetsLastModifiedHeader(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"app.js": &fstest.MapFile{Data: []byte("content"), ModTime: testModTime},
	}
	srv := newTestServer(testFS, preCompressedMap{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)

	srv.ServeHTTP(w, req)

	want := testModTime.UTC().Format(http.TimeFormat)
	got := w.Header().Get("Last-Modified")
	if got != want {
		t.Errorf("Last-Modified = %q, want %q", got, want)
	}
}

func TestFileServerWithCaching_PreCompressedBrotli_SetsContentEncoding(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"app.js.br": &fstest.MapFile{Data: []byte("br-content"), ModTime: testModTime},
	}
	preCompressed := preCompressedMap{
		"/app.js": {br: true},
	}
	srv := newTestServer(testFS, preCompressed)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "br")

	srv.ServeHTTP(w, req)

	if ce := w.Header().Get("Content-Encoding"); ce != "br" {
		t.Errorf("Content-Encoding = %q, want %q", ce, "br")
	}
}

func TestFileServerWithCaching_PreCompressedGzip_SetsContentEncoding(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"app.js.gz": &fstest.MapFile{Data: []byte("gz-content"), ModTime: testModTime},
	}
	preCompressed := preCompressedMap{
		"/app.js": {gz: true},
	}
	srv := newTestServer(testFS, preCompressed)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	srv.ServeHTTP(w, req)

	if ce := w.Header().Get("Content-Encoding"); ce != "gzip" {
		t.Errorf("Content-Encoding = %q, want %q", ce, "gzip")
	}
}

func TestFileServerWithCaching_PreCompressedZstd_SetsContentEncoding(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"app.js.zst": &fstest.MapFile{Data: []byte("zst-content"), ModTime: testModTime},
	}
	preCompressed := preCompressedMap{
		"/app.js": {zstd: true},
	}
	srv := newTestServer(testFS, preCompressed)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "zstd")

	srv.ServeHTTP(w, req)

	if ce := w.Header().Get("Content-Encoding"); ce != "zstd" {
		t.Errorf("Content-Encoding = %q, want %q", ce, "zstd")
	}
}

func TestFileServerWithCaching_PreCompressed_SetsVaryHeader(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"app.js.br": &fstest.MapFile{Data: []byte("br-content"), ModTime: testModTime},
	}
	preCompressed := preCompressedMap{
		"/app.js": {br: true},
	}
	srv := newTestServer(testFS, preCompressed)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "br")

	srv.ServeHTTP(w, req)

	if vary := w.Header().Get("Vary"); vary == "" {
		t.Error("expected Vary header to be set for pre-compressed assets")
	}
}

func TestFileServerWithCaching_PreCompressed_NoAcceptEncoding_NoContentEncoding(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"app.js":    &fstest.MapFile{Data: []byte("content"), ModTime: testModTime},
		"app.js.br": &fstest.MapFile{Data: []byte("br-content"), ModTime: testModTime},
	}
	preCompressed := preCompressedMap{
		"/app.js": {br: true},
	}
	srv := newTestServer(testFS, preCompressed)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)

	srv.ServeHTTP(w, req)

	if ce := w.Header().Get("Content-Encoding"); ce != "" {
		t.Errorf("expected no Content-Encoding without Accept-Encoding, got %q", ce)
	}
}

func TestFileServerWithCaching_NoPreCompressedEntry_NoCacheVaryHeader(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"app.js": &fstest.MapFile{Data: []byte("content"), ModTime: testModTime},
	}
	srv := newTestServer(testFS, preCompressedMap{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("Accept-Encoding", "br, gzip")

	srv.ServeHTTP(w, req)

	if vary := w.Header().Get("Vary"); vary != "" {
		t.Errorf("expected no Vary header for files without pre-compressed variants, got %q", vary)
	}
	if ce := w.Header().Get("Content-Encoding"); ce != "" {
		t.Errorf("expected no Content-Encoding for files without pre-compressed variants, got %q", ce)
	}
}

func TestFileServerWithCaching_DefaultCacheControl(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"app.js": &fstest.MapFile{Data: []byte("content"), ModTime: testModTime},
	}
	srv := newTestServer(testFS, preCompressedMap{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)

	srv.ServeHTTP(w, req)

	if cc := w.Header().Get("Cache-Control"); cc != defaultCacheHeader {
		t.Errorf("Cache-Control = %q, want %q", cc, defaultCacheHeader)
	}
}

func TestFileServerWithCaching_RespectsPreSetCacheControl(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>"), ModTime: testModTime},
	}
	srv := newTestServer(testFS, preCompressedMap{})

	w := httptest.NewRecorder()
	// Caller pre-sets Cache-Control: no-store (as the handler does for index.html).
	w.Header().Set("Cache-Control", "no-store")
	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)

	srv.ServeHTTP(w, req)

	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
	}
}

func TestFileServerWithCaching_IndexHtml_PreCompressedBrotli(t *testing.T) {
	t.Parallel()

	testFS := fstest.MapFS{
		"index.html.br": &fstest.MapFile{Data: []byte("br-html"), ModTime: testModTime},
	}
	preCompressed := preCompressedMap{
		"/index.html": {br: true},
	}
	srv := newTestServer(testFS, preCompressed)

	w := httptest.NewRecorder()
	w.Header().Set("Cache-Control", "no-store")
	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	req.Header.Set("Accept-Encoding", "br")

	srv.ServeHTTP(w, req)

	if ce := w.Header().Get("Content-Encoding"); ce != "br" {
		t.Errorf("Content-Encoding = %q, want %q", ce, "br")
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q (no-store must be preserved)", cc, "no-store")
	}
	if vary := w.Header().Get("Vary"); vary == "" {
		t.Error("expected Vary header for pre-compressed index.html")
	}
}
