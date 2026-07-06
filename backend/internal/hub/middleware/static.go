package middleware

import (
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/gin-gonic/gin"
)

const (
	defaultCacheHeader = "public, max-age=86400" // 24 hours
	indexHTMLPath      = "index.html"
	encodingZstd       = "zstd"
	extensionZstd      = "zst"
	encodingGzip       = "gzip"
	extensionGzip      = "gz"
	zstdSuffix         = ".zst"
)

type preCompressedEntry struct {
	br   bool
	gz   bool
	zstd bool
}

type preCompressedMap map[string]preCompressedEntry

func RegisterStatic(router *gin.Engine) error {
	frontendFS := os.DirFS("./frontend/dist")

	buildTime, err := time.Parse(time.RFC3339, version.BuildDate)
	if err != nil {
		// Fallback during dev where BuildDate is "unknown"
		buildTime = time.Now()
	}

	preCompressed, err := listPreCompressedAssets(frontendFS)
	if err != nil {
		return fmt.Errorf("failed to index pre-compressed assets: %w", err)
	}

	fileServer := NewFileServerWithCaching(http.FS(frontendFS), preCompressed, buildTime)

	handler := func(c *gin.Context) {
		reqPath := strings.TrimPrefix(c.Request.URL.Path, "/")

		// Remove trailing slashes
		if strings.HasSuffix(reqPath, "/") {
			c.Redirect(http.StatusFound, strings.TrimRight(c.Request.URL.String(), "/"))
			return
		}

		// Return 404 for API routes
		if strings.HasPrefix(reqPath, "api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "This endpoint does not exist"})
			return
		}

		// Fallback to index.html for SPA routes
		if reqPath == "" {
			reqPath = indexHTMLPath
		} else if _, err := fs.Stat(frontendFS, reqPath); os.IsNotExist(err) {
			reqPath = indexHTMLPath
		}

		if reqPath == indexHTMLPath {
			// Do not cache index.html to ensure clients always get the latest version
			c.Header("Cache-Control", "no-store")
		}

		c.Request.URL.Path = "/" + reqPath
		fileServer.ServeHTTP(c.Writer, c.Request)
	}

	router.NoRoute(handler)

	return nil
}

type FileServerWithCaching struct {
	fileServer              http.Handler
	lastModified            time.Time
	lastModifiedHeaderValue string
	preCompressedFiles      preCompressedMap
}

func NewFileServerWithCaching(root http.FileSystem, preCompressed preCompressedMap, modTime time.Time) *FileServerWithCaching {
	return &FileServerWithCaching{
		fileServer:              http.FileServer(root),
		lastModified:            modTime,
		lastModifiedHeaderValue: modTime.UTC().Format(http.TimeFormat),
		preCompressedFiles:      preCompressed,
	}
}

func (f *FileServerWithCaching) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ifModifiedSince := r.Header.Get("If-Modified-Since")
	if ifModifiedSince != "" {
		ifModifiedSinceTime, err := time.Parse(http.TimeFormat, ifModifiedSince)
		if err == nil && f.lastModified.Before(ifModifiedSinceTime.Add(1*time.Second)) {
			// The asset hasn't changed since the client's cached version
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	w.Header().Set("Last-Modified", f.lastModifiedHeaderValue)
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", defaultCacheHeader)
	}

	// Check if the asset is available pre-compressed
	available, ok := f.preCompressedFiles[r.URL.Path]
	if ok {
		// Inform proxies and CDNs that the response may vary based on Accept-Encoding
		w.Header().Add("Vary", "Accept-Encoding")

		// Try to select the best encoding based on the client's Accept-Encoding header
		ext, ce := f.selectEncoding(r, available)
		if ext != "" {
			ct := mime.TypeByExtension(path.Ext(r.URL.Path))
			if ct != "" {
				w.Header().Set("Content-Type", ct)
			}

			w.Header().Set("Content-Encoding", ce)
			r.URL.Path += "." + ext
		}
	}

	f.fileServer.ServeHTTP(w, r)
}

func listPreCompressedAssets(distFS fs.FS) (preCompressedMap, error) {
	preCompressedFiles := make(preCompressedMap, 0)
	err := fs.WalkDir(distFS, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			return nil
		}

		switch {
		case strings.HasSuffix(path, ".br"):
			originalPath := "/" + strings.TrimSuffix(path, ".br")
			entry := preCompressedFiles[originalPath]
			entry.br = true
			preCompressedFiles[originalPath] = entry
		case strings.HasSuffix(path, ".gz"):
			originalPath := "/" + strings.TrimSuffix(path, ".gz")
			entry := preCompressedFiles[originalPath]
			entry.gz = true
			preCompressedFiles[originalPath] = entry
		case strings.HasSuffix(path, zstdSuffix):
			originalPath := "/" + strings.TrimSuffix(path, zstdSuffix)
			entry := preCompressedFiles[originalPath]
			entry.zstd = true
			preCompressedFiles[originalPath] = entry
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return preCompressedFiles, nil
}

func (f *FileServerWithCaching) selectEncoding(r *http.Request, available preCompressedEntry) (ext string, contentEnc string) {
	acceptEncoding := strings.TrimSpace(strings.ToLower(r.Header.Get("Accept-Encoding")))
	if acceptEncoding == "" {
		return "", ""
	}

	supportsEncoding := func(encoding string) bool {
		return acceptEncoding == "*" || acceptEncoding == encoding || strings.Contains(acceptEncoding, encoding)
	}

	if available.zstd && supportsEncoding(encodingZstd) {
		return extensionZstd, encodingZstd
	}
	if available.br && supportsEncoding("br") {
		return "br", "br"
	}
	if available.gz && supportsEncoding(encodingGzip) {
		return extensionGzip, encodingGzip
	}

	return "", ""
}
