#!/bin/sh

dir="$(dirname "$(realpath "$0")")/dist"
dists="$(go tool dist list)"
nr_dists="$(echo "$dists" |wc -l)"
re='^(.+)/(.+)$'
i=0
echo "$dists" |while read -r dist; do
    i=$(($i + 1))
    echo "$dist ($i/$nr_dists)"
    os="$(echo "$dist" |sed -r "s $re \\1 ")"
    arch="$(echo "$dist" |sed -r "s $re \\2 ")"
    name="sshmirror-$os-$arch"
    if [ "$os" = "windows" ]; then name="$name.exe"; fi
    if ! env \
        GOOS="$os" \
        GOARCH="$arch" \
        GO111MODULE=on \
        go build -o "$dir/$name"
    then echo "Build failed for $dist" >&2; fi
done
