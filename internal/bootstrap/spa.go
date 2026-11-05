package bootstrap

import (
	"io/fs"
	stdhttp "net/http"
	"path"
	"strings"

	"dosh/web"
)

func newSPAHandler() (stdhttp.Handler, error) {
	content, err := web.FS()
	if err != nil {
		return nil, err
	}
	return &spaHandler{content: content}, nil
}

type spaHandler struct {
	content fs.FS
}

func (h *spaHandler) ServeHTTP(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet && r.Method != stdhttp.MethodHead {
		stdhttp.NotFound(w, r)
		return
	}
	requestPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if requestPath == "." || requestPath == "" {
		requestPath = "index.html"
	}
	if info, err := fs.Stat(h.content, requestPath); err == nil && !info.IsDir() {
		if isAsset(r.URL.Path) {
			w.Header().Set("Cache-Control", cacheControl(true))
		} else {
			w.Header().Set("Cache-Control", cacheControl(false))
		}
		stdhttp.ServeContent(w, r, requestPath, info.ModTime(), mustOpen(h.content, requestPath))
		return
	}
	index, err := fs.ReadFile(h.content, "index.html")
	if err != nil {
		stdhttp.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(stdhttp.StatusOK)
	_, _ = w.Write(index)
}

func mustOpen(content fs.FS, name string) *readSeekCloser {
	file, err := content.Open(name)
	if err != nil {
		panic(err)
	}
	return &readSeekCloser{file: file}
}

type readSeekCloser struct {
	file fs.File
}

func (r *readSeekCloser) Read(p []byte) (int, error) { return r.file.Read(p) }
func (r *readSeekCloser) Seek(offset int64, whence int) (int64, error) {
	seeker, ok := r.file.(interface {
		Seek(int64, int) (int64, error)
	})
	if !ok {
		return 0, fs.ErrInvalid
	}
	return seeker.Seek(offset, whence)
}
func (r *readSeekCloser) Close() error { return r.file.Close() }
