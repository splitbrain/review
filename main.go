package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"

	"codereview/internal/server"
	"codereview/internal/store"
)

//go:embed all:frontend
var frontendFS embed.FS

func main() {
	port := flag.Int("port", 7070, "HTTP server port")
	dir := flag.String("dir", ".", "Root directory to review")
	flag.Parse()

	rootDir, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatalf("Failed to resolve directory: %v", err)
	}

	mdPath := filepath.Join(rootDir, "review.md")
	st, err := store.Load(mdPath, rootDir)
	if err != nil {
		log.Fatalf("Failed to load review data: %v", err)
	}

	subFS, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		log.Fatalf("Failed to create sub filesystem: %v", err)
	}

	handler := server.New(st, rootDir, subFS)

	addr := fmt.Sprintf(":%d", *port)
	fmt.Printf("Code Review running at http://localhost%s\n", addr)
	fmt.Printf("Reviewing: %s\n", rootDir)
	fmt.Printf("Annotations: %s\n", st.MdPath())

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
