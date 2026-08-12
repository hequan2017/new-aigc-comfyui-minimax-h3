package static

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler 返回前端静态资源 handler（SPA 回退到 index.html）
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(sub, path); err != nil {
			// SPA fallback：直接输出 index.html。
			// 不能改 URL.Path 交给 FileServer——"…/index.html" 会触发其 301 重定向循环。
			data, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			// gin 的 NoRoute 可能已预设 404 状态码，这里显式覆盖为 200
			w.WriteHeader(http.StatusOK)
			w.Write(data)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
