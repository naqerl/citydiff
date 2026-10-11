#!/usr/bin/env python3
"""Secondary check on a recorded video: is it padded?

A recorder that cannot render fast enough pads its output to the nominal frame rate with
duplicate frames. The container still reports 25 fps, so ffprobe lies and the defect only
shows up as judder on playback. This counts the frames that actually differ from the one before
them and reports the frame rate inside contiguous runs of moving frames.

Read it as a sanity check, not a measurement. A lossy VP9 -> H.264 round trip puts a small
floor under every frame difference, so a fixed threshold both over-counts motion on a padded
file and under-counts slow camera drift on a clean one. A `median` near 25 means "no obvious
padding"; a median in single digits means the source could not keep up. When the answer matters,
use `record.mjs --check URL`, which counts frames the browser actually delivered and so cannot
be fooled by the codec.

A whole-file average is misleading the other way too: showcase videos hold on each step on
purpose so a human can read it, so a high share of held frames is expected and is not a defect.

    python3 motion.py OUT.mp4 [OUT2.mp4 ...]

Exits 1 if any file looks padded.
"""
import os
import re
import subprocess
import sys
import tempfile

MOVING = 0.35          # mean luma of frame-to-frame difference; below = held
MIN_RUN = 8            # frames; shorter runs are flicker, not animation
NOMINAL_FPS = 25       # what the recorder writes


def frame_deltas(path):
    """Mean luma of each frame minus the one before it, via a null ffmpeg run."""
    with tempfile.NamedTemporaryFile("r", suffix=".txt", delete=False) as fh:
        out = fh.name
    subprocess.run(
        ["ffmpeg", "-v", "error", "-i", path,
         "-vf", "tblend=all_mode=difference,signalstats,"
                f"metadata=print:key=lavfi.signalstats.YAVG:file={out}",
         "-f", "null", "-"],
        check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    raw = open(out).read()
    os.unlink(out)
    return [float(m) for m in re.findall(r"YAVG=([0-9.]+)", raw)]


def duration(path):
    return float(subprocess.run(
        ["ffprobe", "-v", "error", "-show_entries", "format=duration",
         "-of", "csv=p=0", path],
        check=True, capture_output=True, text=True).stdout.strip())


def runs_of(moving):
    runs, start = [], None
    for i, m in enumerate(moving):
        if m and start is None:
            start = i
        elif not m and start is not None:
            runs.append((start, i))
            start = None
    if start is not None:
        runs.append((start, len(moving)))
    return [r for r in runs if r[1] - r[0] >= MIN_RUN]


def analyse(path):
    deltas = frame_deltas(path)
    dur = duration(path)
    runs = runs_of([v >= MOVING for v in deltas])
    if not runs:
        return None, dur, len(deltas)

    fps, moving_s = [], 0.0
    for a, b in runs:
        span = (b - a) / NOMINAL_FPS
        moving_s += span
        fps.extend([(b - a) / span] * (b - a))
    fps.sort()
    return {
        "runs": len(runs),
        "moving_s": moving_s,
        "min": fps[0],
        "median": fps[len(fps) // 2],
        "max": fps[-1],
    }, dur, len(deltas)


def main(paths):
    bad = 0
    for path in paths:
        stats, dur, nframes = analyse(path)
        name = os.path.basename(path)
        if stats is None:
            print(f"{name:34} {dur:7.1f}s  {nframes:6d} frames  no moving runs")
            continue
        held = 100 * (dur - stats["moving_s"]) / dur
        flag = "" if stats["median"] >= 20 else "  <-- JUDDERY"
        if flag:
            bad += 1
        print(f"{name:34} {dur:7.1f}s  fps while moving: "
              f"median {stats['median']:5.1f}  min {stats['min']:5.1f}  "
              f"max {stats['max']:5.1f}  | {stats['moving_s']:5.1f}s moving in "
              f"{stats['runs']:3d} runs, {held:.0f}% held{flag}")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
