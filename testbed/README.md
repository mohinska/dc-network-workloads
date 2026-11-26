# Testbed

Fixed leaf-spine topology in [containerlab](https://containerlab.dev): the "given network" of the project.
Do not change it after Checkpoint 1, so results stay comparable. Diagram: `docs/img/topology.png`.

```
        spine1 (AS 65000)    spine2 (AS 65000)       FRR, eBGP, ECMP over both spines
          │  ╲               ╱  │
        leaf1 (AS 65001)    leaf2 (AS 65002)
        │    │    │         │    │    │
        h1   h2   h3        h4   h5   h6             Linux hosts: iperf3, netperf, sockperf, sshd
```

## Two networks

| | Interface | Addresses | Used for |
|---|---|---|---|
| mgmt | `eth0` | `172.20.20.0/24`, names `clab-netbench-<node>` | Controller SSH, copying files. **Never load or probe it:** it goes through the Docker bridge, not through the fabric |
| data | `eth1`+ | `h<N>`: `10.0.<N>.2/24`, gateway `10.0.<N>.1` on its leaf | All load generators and probes. Always use data IPs |

Fabric links are `10.255.<leaf><spine>.0/31` (`10.255.11.0/31` = leaf1–spine1). Leaf1 serves `10.0.1–3.0/24`,
leaf2 `10.0.4–6.0/24`. A host reaches all of `10.0.0.0/16` through its leaf; its default route stays on mgmt.

| Path | Route |
|---|---|
| h1 → h2 (same leaf) | h1 → leaf1 → h2 |
| h1 → h4 (across spine) | h1 → leaf1 → spine1 **or** spine2 → leaf2 → h4 |

## Run (Linux)

Needs Docker, containerlab and the FRR image (`docker pull quay.io/frrouting/frr:10.7.1`).

```sh
make testbed-up        # builds the netbench-host image, creates testbed/.ssh key, deploys (root or clab_admins)
make testbed-down
```

SSH from the controller (your machine):

```sh
ssh -i testbed/.ssh/id_ed25519 -o StrictHostKeyChecking=no root@clab-netbench-h1
```

macOS: run everything inside a Linux VM (OrbStack / Docker Desktop with a Linux VM); containerlab does not run natively.

## Link profiles

`tc` shapes only egress, so each profile shapes **both ends** of every link. They change `tc` on a running lab.

| Script | Effect |
|---|---|
| `profiles/clean.sh` | All links 1 Gbit, no loss. Leaf oversubscription 3:2 |
| `profiles/oversub.sh` | Uplinks 500 Mbit → 3:1 (`UPLINK_RATE=…` to change) |
| `profiles/lossy.sh [loss%]` | `clean` plus random loss on every leaf–spine link end (default 1 %, end-to-end about twice that) |

Calibration: if CPU of the Docker host or a router limits iperf3 at 1 Gbit, lower the rate (`RATE=500mbit profiles/clean.sh`)
so that the network, not CPU, is the bottleneck.

## Faults and knobs

```sh
# loss / delay on one link end (replace the whole qdisc tree of that interface)
docker exec clab-netbench-leaf1 tc qdisc replace dev eth1 root netem loss 2% delay 5ms
# link failure
docker exec clab-netbench-leaf1 ip link set eth1 down
# container CPU limit
docker update --cpus 0.5 clab-netbench-h1
# ECMP hash: 1 = 5-tuple (default here, high entropy), 0 = src/dst IP only (low entropy, like AI backend flows)
docker exec clab-netbench-leaf1 sysctl -w net.ipv4.fib_multipath_hash_policy=0
```

## Check that it works

```sh
docker exec clab-netbench-h1 ping -c3 10.0.4.2                 # data IP across the spine
docker exec clab-netbench-h1 traceroute -n 10.0.4.2            # leaf1 -> spine -> leaf2 -> h4
docker exec clab-netbench-leaf1 vtysh -c 'show ip route 10.0.4.0/24'   # two next-hops
```

Under `iperf3 -P 8` (h4 runs `iperf3 -s`), bytes grow on both uplinks of leaf1 (`ip -s link show eth1 eth2`).
