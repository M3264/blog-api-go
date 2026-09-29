package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web/*
var playgroundFiles embed.FS

func mountPlayground(mux *http.ServeMux) {
	assets, err := fs.Sub(playgroundFiles, "web")
	if err != nil {
		panic(err)
	}
	fileServer := http.StripPrefix("/playground/", http.FileServer(http.FS(assets)))
	mux.HandleFunc("GET /playground", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/playground/", http.StatusTemporaryRedirect)
	})
	mux.Handle("GET /playground/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' https: http: data:; connect-src 'self'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		fileServer.ServeHTTP(w, r)
	}))
}
