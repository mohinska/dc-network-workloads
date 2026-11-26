# Shared helpers for link profiles. Source it: . "$(dirname "$0")/lib.sh"
# tc shapes EGRESS only, so every link is shaped at both of its ends (both directions).
LAB=${LAB:-clab-netbench}
UPLINKS="leaf1:eth1 spine1:eth1 leaf1:eth2 spine2:eth1 leaf2:eth1 spine1:eth2 leaf2:eth2 spine2:eth2"
HOSTLINKS="h1:eth1 leaf1:eth3 h2:eth1 leaf1:eth4 h3:eth1 leaf1:eth5 h4:eth1 leaf2:eth3 h5:eth1 leaf2:eth4 h6:eth1 leaf2:eth5"

# shape "<node:dev ...>" <rate> [netem args...]
# tbf at <rate>; netem (loss, delay, ...) as its child when args are given.
# The root is recreated each time: deleting only the child would leave tbf with a noop child that drops everything.
shape() {
    eps=$1 rate=$2
    shift 2
    for ep in $eps; do
        node=${ep%%:*} dev=${ep##*:}
        docker exec "$LAB-$node" tc qdisc del dev "$dev" root 2>/dev/null || true
        docker exec "$LAB-$node" tc qdisc add dev "$dev" root handle 1: tbf rate "$rate" burst 256kb latency 20ms
        if [ $# -gt 0 ]; then
            docker exec "$LAB-$node" tc qdisc replace dev "$dev" parent 1:1 handle 10: netem "$@"
        fi
    done
}
