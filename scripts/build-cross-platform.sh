#!/bin/sh
set -eu

printf '%s\n' 'Fyne uses native graphics toolchains; building only for the current host OS.' >&2
exec "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/build-native.sh" "$@"
