#!/bin/sh
# Uplinks at half speed: 3 hosts x 1G down vs 2 x 500M up = 3:1 oversubscription.
. "$(dirname "$0")/lib.sh"
shape "$HOSTLINKS" 1gbit
shape "$UPLINKS" "${UPLINK_RATE:-500mbit}"
