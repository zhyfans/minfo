package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"time"
)

//go:embed webui/dist/*
var staticFS embed.FS

func main() {
	port := getenv("PORT", defaultPort)
	preloadCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := loadUDFModule(preloadCtx); err != nil {
		log.Printf("udf auto-load skipped: %v", err)
	}
	cancel()

	sub, err := fs.Sub(staticFS, "webui/dist")
	if err != nil {
		log.Fatalf("failed to load web UI assets: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/mediainfo", mediainfoHandler("MEDIAINFO_BIN", "mediainfo"))
	mux.HandleFunc("/api/bdinfo", bdinfoHandler("BDINFO_BIN", "bdinfo"))
	mux.HandleFunc("/api/screenshots", screenshotsHandler)
	mux.HandleFunc("/api/path", pathSuggestHandler)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: logging(authenticate(mux)),
	}

	log.Printf("minfo listening on http://localhost:%s", port)
	log.Fatal(server.ListenAndServe())
}
