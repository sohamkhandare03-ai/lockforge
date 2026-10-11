#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$DIR"

if [ ! -f ./lockforge-benchmark ]; then
  go build -o lockforge-benchmark ./cmd/lockforge-benchmark
fi

./lockforge-benchmark -c 10 -n 200
