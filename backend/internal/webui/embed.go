package Webui

import (
	Embed "embed"
	Fs "io/fs"
	Http "net/http"
	Path "path"
	Strings "strings"
)

//go:embed all:dist
var Assets Embed.FS

func Handler() Http.Handler {
	Root, Err := Fs.Sub(Assets, "dist")
	if Err != nil {
		panic("embedded frontend directory is missing")
	}
	return ServeAssets(Root)
}

func ServeAssets(Root Fs.FS) Http.Handler {
	Files := Http.FileServer(Http.FS(Root))
	return Http.HandlerFunc(func(W Http.ResponseWriter, R *Http.Request) {
		if R.Method != Http.MethodGet && R.Method != Http.MethodHead {
			W.Header().Set("Allow", "GET, HEAD")
			Http.Error(W, "Method not allowed", Http.StatusMethodNotAllowed)
			return
		}
		Name := Strings.TrimPrefix(Path.Clean("/"+R.URL.Path), "/")
		for _, Part := range Strings.Split(Name, "/") {
			if Strings.HasPrefix(Part, ".") {
				Http.NotFound(W, R)
				return
			}
		}
		if Name != "" {
			if Info, Err := Fs.Stat(Root, Name); Err == nil && !Info.IsDir() {
				Files.ServeHTTP(W, R)
				return
			}
			if Path.Ext(Name) != "" {
				Http.NotFound(W, R)
				return
			}
		}
		Index, Err := Fs.ReadFile(Root, "index.html")
		if Err != nil {
			Http.Error(W, "Frontend build unavailable; run make build or use Vite on port 5173", Http.StatusServiceUnavailable)
			return
		}
		W.Header().Set("Content-Type", "text/html; charset=utf-8")
		W.Header().Set("Cache-Control", "no-cache")
		if R.Method != Http.MethodHead {
			_, _ = W.Write(Index)
		}
	})
}
