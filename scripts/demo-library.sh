#!/usr/bin/env bash
# Makes a small fake library (movies, TV, music) with artwork for trying Matinee
# out or taking screenshots. Needs ffmpeg.  Usage: scripts/demo-library.sh [dir]
set -euo pipefail
root=${1:-./demo-media}
mkdir -p "$root"/{movies,tv,music}
ff() { ffmpeg -v error -y "$@"; }

colors=(0x1d2b53 0x7e2553 0x008751 0xab5236 0x5f574f 0x29adff 0xff77a8 0xffa300 0x83769c 0x065ab5)
art() { # art <file> <size> <seed>
  local c0=${colors[$(( $3 % 10 ))]} c1=${colors[$(( ($3 + 3) % 10 ))]} c2=${colors[$(( ($3 + 6) % 10 ))]}
  ff -f lavfi -i "gradients=s=$2:c0=$c0:c1=$c1:c2=$c2:n=3:type=radial:seed=$3" -frames:v 1 "$1"
}
video() { # video <file> <seconds> <codec>
  ff -f lavfi -i "testsrc2=s=1280x720:r=24:d=$2" -f lavfi -i "sine=f=330:d=$2" \
    -c:v "$3" -preset ultrafast -pix_fmt yuv420p -c:a aac -shortest "$1" 2>/dev/null ||
  ff -f lavfi -i "testsrc2=s=1280x720:r=24:d=$2" -f lavfi -i "sine=f=330:d=$2" -c:v "$3" -c:a aac -shortest "$1"
}

subs() { # subs <file> <seconds> <lines...>: one cue every few seconds
  local f=$1 secs=$2 n=0 t=1; shift 2
  : > "$f"
  while [ $t -lt $((secs - 2)) ]; do
    n=$((n + 1))
    printf '%d\n00:00:%02d,000 --> 00:00:%02d,500\n%s\n\n' $n $t $((t + 2)) "${@:$(( (n - 1) % $# + 1 )):1}" >> "$f"
    t=$((t + 4))
  done
}

i=0
for m in "Big Buck Bunny (2008)" "Sintel (2010)" "Tears of Steel (2012)" "Elephants Dream (2006)" \
         "Cosmos Laundromat (2015)" "Spring (2019)" "Agent 327 (2017)" "Caminandes (2013)" \
         "Glass Half (2015)" "Charge (2022)" "Sprite Fright (2021)" "Hero (2018)"; do
  d="$root/movies/$m"; mkdir -p "$d"
  [ -f "$d/$m.mp4" ] || video "$d/$m.mp4" 30 libx264
  subs "$d/$m.en.srt" 30 "The lights go down." "<i>Somewhere, a projector hums.</i>" "Popcorn, anyone?"
  subs "$d/$m.es.srt" 30 "Se apagan las luces." "<i>En algún lugar, zumba un proyector.</i>" "¿Palomitas?"
  art "$d/poster.jpg" 500x750 $i; art "$d/fanart.jpg" 1280x720 $((i + 1)); i=$((i + 1))
done

# Sintel's special features: a commentary track inside the movie, and extras
# next to it in folders and with suffixes.
extra() { # extra <file> <lavfi source> <seconds>
  mkdir -p "$(dirname "$1")"
  [ -f "$1" ] || ff -f lavfi -i "$2" -f lavfi -i "sine=f=440:d=$3" -t "$3" -c:v libx264 -preset ultrafast -pix_fmt yuv420p -c:a aac -shortest "$1"
}
d="$root/movies/Sintel (2010)"
if ! ffprobe -v error -select_streams a -show_entries stream=index -of csv=p=0 "$d/Sintel (2010).mp4" | grep -q 2; then
  ff -i "$d/Sintel (2010).mp4" -f lavfi -i "sine=f=220:d=30" -map 0:v -map 0:a -map 1:a -c:v copy -c:a aac -shortest \
    -metadata:s:a:0 language=eng -metadata:s:a:1 language=eng -metadata:s:a:1 title="Director's Commentary" \
    -disposition:a:0 default -disposition:a:1 comment "$d/tmp.mp4" && mv "$d/tmp.mp4" "$d/Sintel (2010).mp4"
fi
extra "$d/Behind The Scenes/Making the dragon.mp4" "mandelbrot=s=1280x720:r=24" 12
extra "$d/Deleted Scenes/The market.mp4" "smptehdbars=s=1280x720:r=24" 8
extra "$d/Interviews/The director.mp4" "gradients=s=1280x720:r=24:speed=0.05" 10
extra "$d/Sintel (2010)-featurette.mp4" "cellauto=s=1280x720:r=24:rule=110" 15

for s in "Night Kitchen" "Coastline"; do
  mkdir -p "$root/tv/$s"; art "$root/tv/$s/poster.jpg" 500x750 $i; art "$root/tv/$s/fanart.jpg" 1280x720 $((i + 2)); i=$((i + 1))
  for season in 1 2; do
    mkdir -p "$root/tv/$s/Season $season"
    for e in 1 2 3 4; do
      f="$root/tv/$s/Season $season/$s - S0${season}E0${e}.mkv"
      [ -f "$f" ] || video "$f" 20 mpeg4
    done
  done
done

for a in "Aurora Static:Signal Fires" "The Midnight Reels:Late Show"; do
  artist=${a%%:*}; album=${a##*:}
  d="$root/music/$artist/$album"; mkdir -p "$d"
  art "$d/cover.jpg" 600x600 $i; i=$((i + 1))
  n=1
  for t in "Opening Credits" "Velvet Seats" "Intermission" "Projector Hum" "Encore"; do
    ff -f lavfi -i "sine=f=$((200 + n * 60)):d=20" -metadata artist="$artist" -metadata album="$album" \
      -metadata title="$t" -metadata track=$n "$d/0$n $t.flac"
    n=$((n + 1))
  done
done
echo "Demo library in $root"
