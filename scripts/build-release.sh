#!/usr/bin/env bash
# Builds the release archives and SHA256SUMS for one version into dist/.
#
#   scripts/build-release.sh v0.1.0
#
# Used by .github/workflows/release.yml; also runs locally (needs Go,
# tar, sha256sum and python3 for the zip files).
set -euo pipefail

version="${1:?usage: $0 <version, e.g. v0.1.0>}"
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
	echo "version must look like v1.2.3 or v1.2.3-rc.1, got '$version'" >&2
	exit 2
fi

targets="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"

rm -rf dist
mkdir -p dist
for target in $targets; do
	os="${target%/*}"
	arch="${target#*/}"
	name="x3f_${version#v}_${os}_${arch}"
	dir="dist/$name"
	bin="x3f"
	[ "$os" = windows ] && bin="x3f.exe"

	mkdir -p "$dir"
	# CGO_ENABLED=0: a static binary without C dependencies.
	# -trimpath: no local file paths in the binary.
	# -s -w: no symbol table and debug info (smaller binary).
	CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
		-ldflags "-s -w -X main.version=$version" -o "$dir/$bin" ./cmd/x3f
	cp LICENSE README.md x3f.example.yaml "$dir/"

	if [ "$os" = windows ]; then
		(cd dist && python3 -I -m zipfile -c "$name.zip" "$name")
	else
		tar -C dist --owner=0 --group=0 --numeric-owner -czf "dist/$name.tar.gz" "$name"
	fi
	rm -rf "$dir"
done

(cd dist && sha256sum -- *.tar.gz *.zip > SHA256SUMS)
echo "built:"
ls -l dist
