#!/usr/bin/env bash
# Regenerates vectors/streams. Needs ffmpeg with libx264. Run from vectors/gen.
set -euo pipefail
cd "$(dirname "$0")"
out=../streams
mkdir -p "$out"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# 20 frames, 160x120, keyframe every 10 frames, no B-frames, no AUD.
ffmpeg -loglevel error -y -f lavfi -i "testsrc=size=160x120:rate=10:duration=2" \
  -c:v libx264 -preset ultrafast -tune zerolatency -g 10 -bf 0 -pix_fmt yuv420p \
  -f h264 "$tmp/base.h264"

go run . "$tmp/base.h264" "$out/testsrc-marked.h264"

ffmpeg -loglevel error -y -r 10 -i "$out/testsrc-marked.h264" -c copy "$out/testsrc-marked.mp4"
ffmpeg -loglevel error -y -r 10 -i "$out/testsrc-marked.h264" -c copy \
  -movflags frag_keyframe+empty_moov+default_base_moof "$out/testsrc-marked-frag.mp4"
ffmpeg -loglevel error -y -r 10 -i "$out/testsrc-marked.h264" -c copy "$out/testsrc-marked.flv"
ffmpeg -loglevel error -y -r 10 -i "$out/testsrc-marked.h264" -c copy "$out/testsrc-marked.ts"

ls -l "$out"
