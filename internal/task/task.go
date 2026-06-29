// Package task defines the publish task model shared by manual use and by
// upstream content generators (e.g. the rainwell-creative-editing skill).
package task

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Kind is the note type on Xiaohongshu.
type Kind string

const (
	KindImage Kind = "image" // 图文笔记
	KindVideo Kind = "video" // 视频笔记
)

// Limits are the per-platform caps the creator platform enforces on a note.
type Limits struct {
	Title   int // max title runes
	Content int // max body runes
	Images  int // max images per image note
}

// Platform limits. Xiaohongshu caps titles at 20 runes; Douyin allows 30.
var (
	xhsLimits    = Limits{Title: 20, Content: 1000, Images: 18}
	douyinLimits = Limits{Title: 30, Content: 1000, Images: 35}
)

// LimitsFor returns the caps for a platform key ("xhs" default, "douyin").
func LimitsFor(platform string) Limits {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "douyin", "dy", "抖音":
		return douyinLimits
	default:
		return xhsLimits
	}
}

// PublishTask is one note to publish. It is the single contract between the
// content source (manual JSON or a generator) and the publisher.
type PublishTask struct {
	Kind    Kind     `json:"kind"`              // "image" or "video"
	Title   string   `json:"title"`             // <= 20 runes
	Content string   `json:"content"`           // body, <= 1000 runes
	Images  []string `json:"images,omitempty"`  // absolute image paths (image notes)
	Video   string   `json:"video,omitempty"`   // absolute video path (video notes)
	Cover   string   `json:"cover,omitempty"`   // optional cover image for video notes
	Topics  []string `json:"topics,omitempty"`  // hashtags, with or without leading '#'

	// AIGenerated, when true, declares the content as AI-generated where the
	// platform supports it (抖音 自主声明 → 内容由AI生成).
	AIGenerated bool `json:"ai_generated,omitempty"`
}

// Load reads and validates a task from a JSON file against Xiaohongshu limits.
// Use LoadFor to validate against a specific platform's limits.
func Load(path string) (*PublishTask, error) { return LoadFor(path, "xhs") }

// LoadFor reads a task and validates it against the given platform's limits.
func LoadFor(path, platform string) (*PublishTask, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read task file: %w", err)
	}
	var t PublishTask
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("parse task json: %w", err)
	}
	if t.Kind == "" {
		if t.Video != "" {
			t.Kind = KindVideo
		} else {
			t.Kind = KindImage
		}
	}
	if err := t.ValidateWith(LimitsFor(platform)); err != nil {
		return nil, err
	}
	return &t, nil
}

// Validate checks the task against Xiaohongshu limits and that media files
// exist. Prefer ValidateWith to target a specific platform.
func (t *PublishTask) Validate() error { return t.ValidateWith(xhsLimits) }

// ValidateWith checks the task against the given platform limits and that media
// files exist.
func (t *PublishTask) ValidateWith(lim Limits) error {
	if strings.TrimSpace(t.Title) == "" {
		return fmt.Errorf("title is required")
	}
	if n := utf8.RuneCountInString(t.Title); n > lim.Title {
		return fmt.Errorf("title is %d chars, exceeds limit of %d", n, lim.Title)
	}
	if n := utf8.RuneCountInString(t.Content); n > lim.Content {
		return fmt.Errorf("content is %d chars, exceeds limit of %d", n, lim.Content)
	}

	switch t.Kind {
	case KindImage:
		if len(t.Images) == 0 {
			return fmt.Errorf("image note requires at least one image")
		}
		if len(t.Images) > lim.Images {
			return fmt.Errorf("image note has %d images, exceeds limit of %d", len(t.Images), lim.Images)
		}
		for _, p := range t.Images {
			if err := mustExist(p); err != nil {
				return err
			}
		}
	case KindVideo:
		if t.Video == "" {
			return fmt.Errorf("video note requires a video file")
		}
		if err := mustExist(t.Video); err != nil {
			return err
		}
		if t.Cover != "" {
			if err := mustExist(t.Cover); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unknown kind %q (want %q or %q)", t.Kind, KindImage, KindVideo)
	}
	return nil
}

// NormalizedTopics returns each topic with a single leading '#'.
func (t *PublishTask) NormalizedTopics() []string {
	out := make([]string, 0, len(t.Topics))
	for _, raw := range t.Topics {
		s := strings.TrimSpace(strings.TrimLeft(raw, "#"))
		if s != "" {
			out = append(out, "#"+s)
		}
	}
	return out
}

// MediaFiles returns the absolute media paths to upload for this task.
func (t *PublishTask) MediaFiles() ([]string, error) {
	var src []string
	if t.Kind == KindVideo {
		src = []string{t.Video}
	} else {
		src = t.Images
	}
	abs := make([]string, 0, len(src))
	for _, p := range src {
		a, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("resolve path %q: %w", p, err)
		}
		abs = append(abs, a)
	}
	return abs, nil
}

func mustExist(p string) error {
	if p == "" {
		return fmt.Errorf("empty media path")
	}
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("media file not found: %s", p)
	}
	return nil
}
