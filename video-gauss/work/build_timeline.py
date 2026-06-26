#!/usr/bin/env python3
"""Concatenate per-scene narration into one voiceover.wav and emit timeline.json.

Scene boundaries are derived from the real measured mp3 durations, so the
Remotion render stays frame-accurate regardless of TTS length drift.
"""
import json
import subprocess
import os

HERE = os.path.dirname(os.path.abspath(__file__))
AUDIO = os.path.join(HERE, "audio")
FPS = 30
GAP = 0.35  # seconds of silence between scenes

# Scene script: visual kind + caption lines (shown evenly across the scene).
SCENES = [
    {"id": 1, "mp3": "s1.mp3", "visual": "img:gauss_classroom.png",
     "captions": ["两百多年前，老师想偷懒", "“把 1 加到 100”", "一个 10 岁小孩，几秒就报出答案", "5050"]},
    {"id": 2, "mp3": "s2.mp3", "visual": "anim:PairSum",
     "captions": ["1+100=101，2+99=101…", "一共 50 对", "50 × 101 = 5050", "他叫高斯，后来的“数学王子”"]},
    {"id": 3, "mp3": "s3.mp3", "visual": "img:gauss_portrait.png",
     "captions": ["高斯 1777—1855", "数论 / 几何 / 天文 / 物理", "不到 20 岁", "尺规作出正 17 边形（2000 年难题）"]},
    {"id": 4, "mp3": "s4.mp3", "visual": "anim:BellCurve",
     "captions": ["正态分布 = 高斯分布", "身高 / 成绩 / 误差 都像它", "约 68% 落在 ±1σ 内", "曾印在德国马克纸币上"]},
    {"id": 5, "mp3": "s5.mp3", "visual": "img:ceres_night.png",
     "captions": ["1801 年，谷神星失踪", "高斯算出它会在哪里重现 → 真的找到了", "“数学是科学的女王”", "关注我，看更多数学家的故事"]},
]


def dur(path):
    out = subprocess.check_output(
        ["ffprobe", "-v", "0", "-show_entries", "format=duration",
         "-of", "csv=p=0", path]).decode().strip()
    return float(out)


def main():
    # Build a concat filter with silence padding between scenes.
    inputs = []
    for s in SCENES:
        inputs += ["-i", os.path.join(AUDIO, s["mp3"])]

    # Measure and compute absolute scene boundaries (including gaps).
    t = 0.0
    scenes_out = []
    parts = []  # ffmpeg concat list entries
    silence = os.path.join(AUDIO, "_gap.wav")
    subprocess.run(["ffmpeg", "-y", "-f", "lavfi", "-i",
                    f"anullsrc=r=44100:cl=mono", "-t", str(GAP), silence],
                   check=True, capture_output=True)

    concat_list = os.path.join(AUDIO, "_concat.txt")
    with open(concat_list, "w") as f:
        for i, s in enumerate(SCENES):
            d = dur(os.path.join(AUDIO, s["mp3"]))
            start = t
            end = t + d
            scenes_out.append({
                "id": s["id"],
                "visual": s["visual"],
                "captions": s["captions"],
                "startMs": round(start * 1000),
                "endMs": round(end * 1000),
                "startFrame": round(start * FPS),
                "endFrame": round(end * FPS),
            })
            f.write(f"file '{os.path.join(AUDIO, s['mp3'])}'\n")
            t = end
            if i < len(SCENES) - 1:
                f.write(f"file '{silence}'\n")
                t += GAP

    voiceover = os.path.join(HERE, "voiceover.wav")
    subprocess.run(["ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i",
                    concat_list, "-ar", "44100", "-ac", "1", voiceover],
                   check=True, capture_output=True)

    total = dur(voiceover)
    timeline = {
        "fps": FPS,
        "width": 1080,
        "height": 1440,  # 3:4
        "audioSrc": "voiceover.wav",
        "durationMs": round(total * 1000),
        "durationFrames": round(total * FPS),
        "scenes": scenes_out,
    }
    with open(os.path.join(HERE, "timeline.json"), "w") as f:
        json.dump(timeline, f, ensure_ascii=False, indent=2)

    print(f"voiceover.wav = {total:.2f}s, {timeline['durationFrames']} frames")
    for s in scenes_out:
        print(f"  scene {s['id']:>1} [{s['visual']:<22}] "
              f"{s['startMs']/1000:5.1f}-{s['endMs']/1000:5.1f}s")


if __name__ == "__main__":
    main()
