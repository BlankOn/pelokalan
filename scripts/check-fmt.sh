#!/bin/sh
# Fails when a Go file is not gofmt-formatted (used by make check)
dirs=""
for d in cmd internal pkg; do
	[ -d "$d" ] && dirs="$dirs $d"
done
unformatted=$(gofmt -l $dirs)
if [ -n "$unformatted" ]; then
	echo "$unformatted"
	echo "gofmt: the files above need formatting (gofmt -w <file>)"
	exit 1
fi
