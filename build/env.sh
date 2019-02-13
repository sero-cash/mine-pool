#!/bin/sh

set -e

if [ ! -f "build/env.sh" ]; then
    echo "$0 must be run from the root of the repository."
    exit 2
fi

# Create fake Go workspace if it doesn't exist yet.
workspace="$PWD/build/_workspace"
root="$PWD"
serodir="$workspace/src/github.com/sero-cash"
if [ ! -L "$serodir/mine-pool" ]; then
    mkdir -p "$serodir"
    cd "$serodir"
    ln -s ../../../../../. mine-pool
    cd "$root"
fi

if [ ! -L "$serodir/go-sero" ]; then
    mkdir -p "$serodir"
    cd "$serodir"
    ln -s ../../../../../../go-sero go-sero
    cd "$root"
fi

if [ ! -L "$serodir/go-czero-import" ]; then
    mkdir -p "$serodir"
    cd "$serodir"
    ln -s ../../../../../../go-czero-import go-czero-import
    cd "$root"
fi

# Set up the environment to use the workspace.
# Also add Godeps workspace so we build using canned dependencies.
GOPATH="$workspace"
GOBIN="$PWD/build/bin"
export GOPATH GOBIN

# Run the command inside the workspace.
cd "$serodir/mine-pool"
PWD="$serodir/mine-pool"

# Launch the arguments with the configured environment.
exec "$@"
