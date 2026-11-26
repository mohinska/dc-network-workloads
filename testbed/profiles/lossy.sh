#!/bin/sh
# Like clean, plus random loss on every leaf<->spine link end. Usage: lossy.sh [loss%] (default 1%).
# A packet crossing the fabric passes two such ends per direction, so end-to-end loss is about twice this value.
. "$(dirname "$0")/lib.sh"
shape "$HOSTLINKS" 1gbit
shape "$UPLINKS" 1gbit loss "${1:-1}%"
