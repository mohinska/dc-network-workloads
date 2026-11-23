# Bottleneck diagnosis rules

Which component limits performance, judged by host-side counters collected during the test
(on sender, receiver and hypervisor). Starting point; thresholds to be tuned on measurements.

| Observation | Bottleneck |
|---|---|
| The load generator process itself uses 100 % of one core | **Load tool**, not host or network. Fix the tool setup first (e.g. iperf3 < 3.16 runs all streams in one thread) |
| `nr_throttled` / `throttled_usec` in the container's cgroup `cpu.stat` keep rising | **Container CPU limit**, not the network |
| Throughput plateaus, a core at ~100 % `%soft`/`%sys`, `softnet_stat` time_squeeze ↑, retransmits ≈ 0 | **Host CPU** (packet processing) |
| `softnet_stat` dropped ↑, `ethtool -S` rx_missed / no_buffer ↑ | **Host NIC / ring buffer** |
| Retransmits ↑, UDP loss ↑, CPU has headroom, an idle probe pair sharing the loaded link sees its RTT rise | **Network congestion** (queues in the fabric). If only the loaded host's own RTT rises, the queue is in its own qdisc/socket buffer |
| Out-of-order / SACK-reorder / DSACK ↑, worse with more paths | **Reordering** (ECMP / per-packet spraying) |
| Only one 5-tuple group or one leaf pair is bad | **One path / link** |
| `ip -s link` errors, `ethtool -S` `*err*` counters ↑, TCP resets ↑ | **Link or driver errors** (Err) |

> In a virtual network the switches run on the same physical CPUs as the hosts, so a
> "network" and a "CPU" bottleneck can be the same thing. Always record hypervisor load too.
