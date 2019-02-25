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

mkdir -p "$root/../go-czero-import/czero/lib"
mkdir -p "$root/czero/lib"


if [ $1 == "linux-v3" ]; then
    cd "$root/../go-czero-import/czero"
    rm -rf lib/*
    cp -rf lib_LINUX_AMD64_V3/* lib
    rm -rf $root/czero/lib/*
    cp -rf lib_LINUX_AMD64_V3/* $root/czero/lib
    shift 1
elif [ $1 == "linux-v4" ];then
    cd "$root/../go-czero-import/czero"
    rm -rf lib/*
    cp -rf lib_LINUX_AMD64_V4/* lib
    rm -rf $root/czero/lib/*
    cp -rf lib_LINUX_AMD64_V4/* $root/czero/lib
    shift 1
elif [ $1 == "darwin-amd64" ];then
    cd "$root/../go-czero-import/czero"
    rm -rf lib/*
    cp -rf lib_DARWIN_AMD64/*  lib
    rm -rf $root/czero/lib/*
    cp -rf lib_DARWIN_AMD64/* $root/czero/lib
    shift 1
elif [ $1 == "windows-amd64" ];then
    cd "$root/../go-czero-import/czero"
    rm -rf lib/*
    cp -rf lib_WINDOWS_AMD64/*  lib
    rm -rf $root/czero/lib/*
    cp -rf lib_WINDOWS_AMD64/* $root/czero/lib
    shift 1
else
     echo "default lib"
fi


# Set up the environment to use the workspace.
# Also add Godeps workspace so we build using canned dependencies.
GOPATH="$workspace"
GOBIN="$root/build/bin"
export GOPATH GOBIN

# Run the command inside the workspace.
cd "$serodir/mine-pool"
PWD="$serodir/mine-pool"

echo $PWD

# Launch the arguments with the configured environment.
exec "$@"
