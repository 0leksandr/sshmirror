#!/bin/sh
# TODO: github action/workflow

root="$(dirname "$(realpath "$0")")"
dir="$root/dist"
find "$dir" -type f -not -name .gitignore -delete
re='^(.+)/(.+)$'
~/go/sdk/go1.17.8/bin/go tool dist list |while read -r dist; do
    echo "$dist"
    os="$(echo "$dist" |sed -r "s $re \\1 ")"
    arch="$(echo "$dist" |sed -r "s $re \\2 ")"
    name="sshmirror-$os-$arch"
    if [ "$os" = "windows" ]; then name="$name.exe"; fi
    if ! env \
        GOOS="$os" \
        GOARCH="$arch" \
        GOPATH="$root/gopath" \
        GO111MODULE=on \
        GOROOT=/home/nezhraba/go/sdk/go1.17.8 \
        ~/go/sdk/go1.17.8/bin/go build -o "$dir/$name" -pkgdir "$root/pkgdir"
    then echo "Build failed for $dist" >&2; fi
done
