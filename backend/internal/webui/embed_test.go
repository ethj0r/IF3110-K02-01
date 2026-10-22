package Webui

import (
	Httptest "net/http/httptest"
	Strings "strings"
	Testing "testing"
	Fstest "testing/fstest"
)

func TestFrontendRouting(T *Testing.T) {
	Assets := Fstest.MapFS{
		"index.html":    {Data: []byte("<html>application</html>")},
		"assets/app.js": {Data: []byte("console.log('application')")},
		".gitkeep":      {Data: []byte("hidden")},
	}
	for _, Tc := range []struct {
		Method, Path string
		Status       int
		Contains     string
	}{
		{"GET", "/", 200, "<html>application</html>"},
		{"GET", "/databases/1", 200, "<html>application</html>"},
		{"GET", "/assets/app.js", 200, "console.log"},
		{"GET", "/assets/missing.js", 404, ""},
		{"GET", "/.gitkeep", 404, ""},
		{"POST", "/databases/1", 405, ""},
		{"HEAD", "/databases/1", 200, ""},
	} {
		T.Run(Tc.Method+Tc.Path, func(T *Testing.T) {
			Response := Httptest.NewRecorder()
			ServeAssets(Assets).ServeHTTP(Response, Httptest.NewRequest(Tc.Method, Tc.Path, nil))
			if Response.Code != Tc.Status || !Strings.Contains(Response.Body.String(), Tc.Contains) {
				T.Fatalf("unexpected response: %d %s", Response.Code, Response.Body.String())
			}
			if Tc.Method == "HEAD" && Response.Body.Len() != 0 {
				T.Fatal("HEAD must not return a body")
			}
		})
	}
}

func TestMissingFrontendBuild(T *Testing.T) {
	Response := Httptest.NewRecorder()
	ServeAssets(Fstest.MapFS{}).ServeHTTP(Response, Httptest.NewRequest("GET", "/", nil))
	if Response.Code != 503 {
		T.Fatalf("missing build status = %d, want 503", Response.Code)
	}
}
