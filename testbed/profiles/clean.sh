#!/bin/sh
# All links 1 Gbit, no loss. Leaf oversubscription is 3:2 (3 hosts x 1G down, 2 uplinks x 1G up).
. "$(dirname "$0")/lib.sh"
RATE=${RATE:-1gbit}
shape "$HOSTLINKS $UPLINKS" "$RATE"
