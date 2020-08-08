#!/bin/sh

root="$(dirname "$(realpath "$0")")"
re='^(.+)/(.+)$'
go tool dist list |while read -r dist; do
    echo "$dist"
    os="$(echo "$dist" |sed -r "s $re \\1 ")"
    arch="$(echo "$dist" |sed -r "s $re \\2 ")"
    name="sshstream-$os-$arch"
    if [ "$os" = "windows" ]; then name="$name.exe"; fi
    if ! env GOOS="$os" GOARCH="$arch" GOPATH="$root/gopath" go build -o "$root/dist/$name" -i -pkgdir "$root/pkgdir" ;then
        echo "Build failed for $dist"
    fi
    find build -type d -empty -delete
done
