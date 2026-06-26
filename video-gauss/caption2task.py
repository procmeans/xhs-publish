#!/usr/bin/env python3
"""Bridge: rainwell-creative-editing caption.json + rendered video -> xhs-publish task.json.

Lets the creative-editing skill feed the Go publisher directly:
  caption.json (title/caption_body/tags) + final.mp4  ->  task.json (kind=video)
"""
import argparse
import json
import os
import sys

MAX_TITLE = 20  # Xiaohongshu title rune limit


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--caption", required=True, help="caption.json from generate_caption.py")
    ap.add_argument("--video", required=True, help="rendered video path")
    ap.add_argument("--cover", help="optional cover image")
    ap.add_argument("--title", help="override title")
    ap.add_argument("--topics", help="comma-separated topics override (without #)")
    ap.add_argument("--output", required=True, help="task.json output path")
    args = ap.parse_args()

    with open(args.caption, encoding="utf-8") as f:
        cap = json.load(f)

    video = os.path.abspath(args.video)
    if not os.path.exists(video):
        sys.exit(f"video not found: {video}")

    title = args.title or cap.get("title", "")
    if len(title) > MAX_TITLE:
        title = title[:MAX_TITLE]

    if args.topics:
        topics = [t.strip() for t in args.topics.split(",") if t.strip()]
    else:
        # Strip leading '#' from the skill's tags; the publisher re-adds it.
        topics = [t.lstrip("#") for t in cap.get("tags", [])]

    task = {
        "kind": "video",
        "title": title,
        "content": cap.get("caption_body", ""),
        "video": video,
        "topics": topics,
    }
    if args.cover:
        task["cover"] = os.path.abspath(args.cover)

    with open(args.output, "w", encoding="utf-8") as f:
        json.dump(task, f, ensure_ascii=False, indent=2)
    print(f"task.json -> {args.output}")
    print(f"  title({len(title)}/20): {title}")
    print(f"  topics: {topics}")


if __name__ == "__main__":
    main()
