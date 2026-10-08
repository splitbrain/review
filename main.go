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

	"review/internal/gitstatus"
	"review/internal/server"
	"review/internal/store"
	"review/internal/watcher"
)

//go:embed all:frontend
var frontendFS embed.FS

func main() {
	port := flag.Int("port", 7070, "HTTP server port")
	dir := flag.String("dir", ".", "Root directory to review")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() > 1 {
		flag.Usage()
		os.Exit(2)
	}

	// The setup-review-history subcommand needs no server and no base revision.
	setupHistory := flag.Arg(0) == "setup-review-history"

	rootDir, err := filepath.Abs(*dir)
	if err != nil {
		log.Fatalf("Failed to resolve directory: %v", err)
	}

	if setupHistory {
		runSetupReviewHistory(rootDir)
		return
	}

	var baseCommit gitstatus.Base
	if base := flag.Arg(0); base != "" {
		baseCommit, err = gitstatus.ResolveBase(rootDir, base)
		if err != nil {
			log.Fatalf("Failed to resolve base revision: %v", err)
		}
	}

	mdPath := filepath.Join(rootDir, "REVIEW.md")
	st, err := store.Load(mdPath, rootDir)
	if err != nil {
		log.Fatalf("Failed to load review data: %v", err)
	}
	st.SetBase(baseCommit.Summary(rootDir))

	// Run initial drift check on all annotated files
	drifted, err := st.CheckAllDrift()
	if err != nil {
		log.Printf("Warning: could not write adjusted annotations: %v", err)
	}
	for f := range drifted {
		log.Printf("Drift detected in %s — annotations adjusted", f)
	}

	subFS, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		log.Fatalf("Failed to create sub filesystem: %v", err)
	}

	// Set up WebSocket hub
	hub := server.NewHub()
	go hub.Run()

	// Set up file watcher
	if w, err := watcher.New(st); err != nil {
		log.Printf("Warning: file watching disabled: %v", err)
	} else {
		w.Start()
		defer w.Stop()
		server.BridgeWatcher(hub, st, w)
	}

	handler := server.New(st, rootDir, baseCommit, subFS, hub)

	// Bind to the loopback interface only: a review exposes the whole source
	// tree and has no access control of its own.
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	url := fmt.Sprintf("http://127.0.0.1:%d", *port)
	fmt.Printf("Code Review running at %s\n", url)
	fmt.Printf("Reviewing: %s\n", rootDir)
	if baseCommit.Commit != "" {
		fmt.Printf("Comparing against: %s (%s)\n", baseCommit.Rev, baseCommit.Commit[:7])
	}
	fmt.Printf("Annotations: %s\n", st.MdPath())

	go openBrowser(url)

	srv := &http.Server{Addr: addr, Handler: handler}

	// Graceful shutdown: catch signals, notify clients, then stop
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down...")
		hub.Shutdown()
		time.Sleep(200 * time.Millisecond) // give WS time to deliver
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

// runSetupReviewHistory enables review history for the project at rootDir and
// reports what it created.
func runSetupReviewHistory(rootDir string) {
	created, err := store.SetupReviewHistory(rootDir)
	if err != nil {
		log.Fatalf("Failed to set up review history: %v", err)
	}
	if len(created) == 0 {
		fmt.Println("Review history already set up")
		return
	}
	for _, path := range created {
		fmt.Printf("Created %s\n", path)
	}
	fmt.Println("Starting a new review will now move REVIEW.md into " + store.ReviewDir + "/")
}

// usage prints the command line syntax, including the optional base revision
// that the flag package does not know about.
func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprintf(out, "Usage: %s [flags] [base]\n", filepath.Base(os.Args[0]))
	fmt.Fprintf(out, "       %s [flags] setup-review-history\n\n", filepath.Base(os.Args[0]))
	fmt.Fprint(out, "  base\n    \tBranch, tag or commit to compare against instead of HEAD\n")
	fmt.Fprint(out, "  setup-review-history\n    \tEnable review history: keep retired reviews in "+store.ReviewDir+"/\n")
	flag.PrintDefaults()
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
