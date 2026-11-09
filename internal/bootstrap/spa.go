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
		stdhttp.ServeFileFS(w, r, h.content, requestPath)
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
