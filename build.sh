#!/bin/sh

package="sshstream"
root="$(dirname "$(realpath "$0")")"
re='^(.+)/(.+)$'
go tool dist list |while read -r dist; do
    echo "$dist"
    os="$(echo "$dist" |sed -r "s $re \\1 ")"
    arch="$(echo "$dist" |sed -r "s $re \\2 ")"
    build="$root/build/$os/$arch"
    mkdir -p "$build"
    name="$package"
    if [ "$os" = "windows" ]; then name="$name.exe"; fi
    if ! env GOOS="$os" GOARCH="$arch" GOPATH="$root/gopath" go build -o "$build/$name" -i -pkgdir "$root/pkgdir" ;then
        echo "Build failed for $dist"
        /bin/rm -r "$build"
    fi
    find build -type d -empty -delete
done
