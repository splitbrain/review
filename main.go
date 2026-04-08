package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"review/internal/server"
	"review/internal/store"
	"review/internal/watcher"
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

	mdPath := filepath.Join(rootDir, "REVIEW.md")
	st, err := store.Load(mdPath, rootDir)
	if err != nil {
		log.Fatalf("Failed to load review data: %v", err)
	}

	// Run initial drift check on all annotated files
	if drifted := st.CheckAllDrift(); len(drifted) > 0 {
		for f := range drifted {
			log.Printf("Drift detected in %s — annotations adjusted", f)
		}
	}

	subFS, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		log.Fatalf("Failed to create sub filesystem: %v", err)
	}

	// Set up WebSocket hub
	hub := server.NewHub()
	go hub.Run()

	// Set up file watcher
	w, err := watcher.New(st)
	if err != nil {
		log.Printf("Warning: file watching disabled: %v", err)
	} else {
		w.Start()
		defer w.Stop()

		// Bridge watcher events to WebSocket hub
		go func() {
			for ev := range w.Events() {
				// Build message with current annotation state
				msg := map[string]interface{}{
					"type": ev.Type,
				}
				if ev.Path != "" {
					msg["path"] = ev.Path
					msg["annotations"] = annotationsToResponse(st.GetFile(ev.Path))
				}
				if ev.Type == "review-reloaded" {
					msg["allAnnotations"] = allAnnotationsToResponse(st.All())
				}
				hub.Broadcast(msg)
			}
		}()
	}

	handler := server.New(st, rootDir, subFS, hub)

	addr := fmt.Sprintf(":%d", *port)
	url := fmt.Sprintf("http://localhost:%d", *port)
	fmt.Printf("Code Review running at %s\n", url)
	fmt.Printf("Reviewing: %s\n", rootDir)
	fmt.Printf("Annotations: %s\n", st.MdPath())

	go openBrowser(url)

	srv := &http.Server{Addr: addr, Handler: handler}

	// Graceful shutdown: catch signals, notify clients, then stop
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down...")
		hub.Broadcast(map[string]string{"type": "server-shutdown"})
		time.Sleep(200 * time.Millisecond) // give WS time to deliver
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

// annotationsToResponse converts store annotations to the API response format.
func annotationsToResponse(anns map[int]*store.Annotation) map[string]map[string]interface{} {
	result := make(map[string]map[string]interface{}, len(anns))
	for line, ann := range anns {
		result[fmt.Sprintf("%d", line)] = map[string]interface{}{
			"comment":  ann.Comment,
			"outdated": ann.Outdated,
		}
	}
	return result
}

// allAnnotationsToResponse converts all store annotations to the API response format.
func allAnnotationsToResponse(all map[string]map[int]*store.Annotation) map[string]map[string]map[string]interface{} {
	result := make(map[string]map[string]map[string]interface{}, len(all))
	for file, anns := range all {
		result[file] = annotationsToResponse(anns)
	}
	return result
}

func openBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}

	args = append(args, url)
	exec.Command(cmd, args...).Start()
}

