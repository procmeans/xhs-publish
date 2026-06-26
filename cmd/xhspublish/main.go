// Command xhspublish publishes a single note to Xiaohongshu by driving an
// already-logged-in Chrome over the DevTools Protocol.
//
// Usage:
//
//	# 1. start Chrome once and log into Xiaohongshu (see scripts/start-chrome.sh)
//	# 2. fill the form but stop before 发布 (safe default):
//	xhspublish -task examples/task.json
//	# 3. when it looks right, actually publish:
//	xhspublish -task examples/task.json -publish
package main

import (
	"flag"
	"log"
	"os"
	"time"

	"github.com/procmeans/xhs-publish/internal/publisher"
	"github.com/procmeans/xhs-publish/internal/task"
)

func main() {
	log.SetFlags(log.Ltime)

	var (
		taskPath = flag.String("task", "", "path to the publish task JSON file (required)")
		cdp      = flag.String("cdp", "http://localhost:9222", "Chrome DevTools Protocol endpoint")
		publish  = flag.Bool("publish", false, "actually click 发布 (default is dry-run: fill only)")
		noHuman  = flag.Bool("no-human", false, "disable human-like pauses/mouse paths/typos (faster)")
		timeout  = flag.Duration("timeout", 60*time.Second, "per-step timeout")
	)
	flag.Parse()

	if *taskPath == "" {
		log.Println("error: -task is required")
		flag.Usage()
		os.Exit(2)
	}

	t, err := task.Load(*taskPath)
	if err != nil {
		log.Fatalf("invalid task: %v", err)
	}
	log.Printf("task: kind=%s title=%q topics=%v", t.Kind, t.Title, t.NormalizedTopics())

	opt := publisher.DefaultOptions()
	opt.CDPEndpoint = *cdp
	opt.DryRun = !*publish
	opt.Humanize = !*noHuman
	opt.StepTimeout = *timeout

	// We attach to the user's REAL, visible Chrome over CDP — never headless.
	log.Printf("attaching to real Chrome at %s (not headless); humanize=%v", *cdp, opt.Humanize)

	p, err := publisher.New(opt)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer p.Close()

	if err := p.Publish(t); err != nil {
		log.Fatalf("publish failed: %v", err)
	}

	if opt.DryRun {
		log.Println("done (dry-run). Re-run with -publish to post for real.")
	} else {
		log.Println("done. Note submitted — verify it in the creator center.")
	}
}
