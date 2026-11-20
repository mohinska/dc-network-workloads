# Brief: what we build and how

## 1. The actual goal

Running a benchmark is not enough. When we add threads/streams and throughput drops,
we must be able to say **who is to blame: host CPU / NIC / network**, and if it is the
network, **what exactly** (loss, queueing latency, reordering, one bad path).
The network is a **black box**: we do not configure it, we only push load through it and measure from the hosts.

Target topology (from the whiteboard): leaf-spine, 2 spines → Internet (N-S),
2 leaves (Acc), 3 hosts per leaf (E-W). Metrics on the board: **Loss, Latency, Err, SeqErr (reordering)**.

## 1a. Constraints (updated)

- **The product is the tool**, not a specific network. It measures latency, loss, reordering, duplicates, errors and throughput between endpoints, and says whether the network or the host is to blame.
- **Network-agnostic.** It runs on any Linux endpoints (bare metal, VMs or containers) over **any network**: a real DC fabric or a Docker topology. It never looks inside the network.
- **No topology is provided.** For development and the demo we build our own Docker topology (containerlab: 2 spines, 2 leaves, hosts and an "Inet" node). We use it as a **testbed with known faults** (`tc netem` on links) to prove that the tool detects what we injected.
- **Two measurement modes:** **E-W** (endpoint ↔ endpoint inside the DC) and **N-S** (endpoint ↔ external client/server).

### Docker-specific notes (testbed only; the tool must not depend on them)

- All containers share **one kernel and one CPU pool**. The "network" is CPU on the same machine, so we must record host CPU on every run.
- **cgroup CPU throttling** (`cpu.stat` → `nr_throttled`, `throttled_usec`) is a hidden bottleneck. It looks exactly like "the network got slower". Record it for every container.
- TCP counters (`nstat`, `/proc/net/snmp`) are **per network namespace**, so we get clean per-container numbers.
- `softnet_stat` and NIC counters are effectively **host-wide**, so read them on the Docker host.
- `tc netem` works inside a container with `--cap-add NET_ADMIN`. We need that for calibration.
- Clocks are shared on one host, so one-way delay is valid there for free. Across machines it needs chrony/PTP.
- MPI/RDMA is not realistic here. OSU and GPCNeT become optional; our tool reproduces their traffic patterns (all-to-all, incast, victim + aggressor) instead.

### Our tool (`netbench`): shape

```
netbench controller (Go, same binary)       ── reads scenario YAML, runs it, writes results
  │ SSH (real hosts) or docker exec (testbed)
  ├── netbench agent on every endpoint (.deb / static binary / container) ── deps: iperf3>=3.16 (wrk2 optional)
  │     load:      iperf3 (bulk), built-in UDP prober (loss / RTT / one-way / reorder / dup), wrk2 (HTTP)
  │     collector: nstat, ss -ti, ip -s link, cgroup cpu.stat — snapshot before/after
  └── optional host collector                ── mpstat, softnet_stat; cgroup/docker stats only when endpoints are containers
output: results.jsonl + summary table + verdict (tool | host CPU | cgroup throttle | network: loss/queue/reorder/path)
```

- **Load and probe run at the same time.** A bulk load (iperf3) fills the path while a light UDP probe (our prober, with sequence numbers and timestamps) measures Loss / Latency / SeqErr *under* that load.
- **Per-path view without seeing the network.** Run N probes with different source ports. ECMP hashes them onto different spines, so one bad path shows up as one bad port group.
- The UDP prober is **our own code** (seq + timestamp, one socket per source port), so per-flow results come out natively and we don't depend on irtt.

### Language: Go

- **One static binary** (`netbench agent` / `netbench run`), so there is nothing to install on hosts except iperf3.
- **Packaging is cheap:** `.deb`/`.rpm` via **nfpm** (or goreleaser), plus a tiny container image for the testbed.
- **arm64 cross-compile** in one line (`GOARCH=arm64 go build`), which covers Radxa boards.
- **Is it fast enough?** The controller doesn't need speed. The prober does need precise timestamps at thousands of packets/s, which Python handles poorly and Go handles easily. Bulk line-rate load stays in iperf3 (C) anyway.
- **Rust** would also work and is a bit faster with no GC pauses, but it is slower to write. Pick it only if someone on the team already knows it. GC pauses are a non-issue at probe rates; timestamps are taken right at send/receive.
- **Packaging caveat:** Debian 12 ships iperf3 3.12 (single-threaded). Either require Ubuntu 24.04+/Debian 13, or the agent runs N iperf3 processes itself. We should do the second anyway.

### E-W vs N-S modes

| | **E-W** | **N-S** |
|---|---|---|
| Endpoints | Container ↔ container in the fabric: same leaf, cross-spine | Container in the fabric ↔ "Inet" container outside the uplink |
| Traffic | Bulk (storage/backup), RPC small messages, all-to-all, incast N→1 | Many short HTTP connections (BYOD/mobile clients), downloads, uploads |
| Load | iperf3 `-P` sweep, netbench probes, N→1 iperf3 | wrk2/fortio with many connections; netem on the client side to emulate mobile RTT/loss |
| Key metrics | Throughput, loss, p99 RTT, reorder %, retransmits, per-path balance | RPS, connect time, p99 latency, loss, uplink saturation |
| Typical failure | Spine oversubscription, ECMP imbalance, incast drops | Uplink bottleneck, conntrack/NAT limits, asymmetric paths |

## 2. Method: load + simultaneous telemetry + attribution

Every test = **load generator** + **host-side counters collected at the same time** + **decision rule**.
Collect counters on **sender, receiver and hypervisor**. Reordering costs CPU on the *receiver* (softirq), so watching only one end misses it.

1. **Calibrate the hosts without the network.** Run the same load over loopback / between two VMs on one host.
   This gives the CPU ceiling. If the fabric result is close to it, the network is not the bottleneck.
2. **Locate the problem by path length.** Run the same test across three pairs: same leaf, leaf → spine → leaf,
   and host → Internet (N-S). If quality drops only on the longer paths, the problem is in that fabric segment.
3. **Parallelism sweep** (this reproduces the "more threads = slower" case): streams/ranks = 1, 2, 4 … 64
   (e.g. 2 nodes × 24–32 processes). At every step record throughput **and** all counters below.
4. **ECMP / reordering check.** Vary the 5-tuple (source ports); several flows take different spine paths.
   Look for one slow path and for out-of-order packets that the receiver CPU must put back in order.
5. **Validate the detector with known faults.** On our own links use `tc netem` / `tbf`
   (loss X %, reorder Y %, rate cap). The tool must report the fault we injected. If it cannot,
   its "network vs CPU" verdict cannot be trusted either.

### Decision rule (first version, to be tuned by step 5)

| What we see | Verdict |
|---|---|
| The load generator process itself uses 100 % of one core | **Tool bottleneck**, not host or network. Fix the tool setup first |
| `nr_throttled` / `throttled_usec` in the container's cgroup `cpu.stat` keep rising | **Container CPU limit**, not the network |
| Throughput plateaus, a core at ~100 % `%soft`/`%sys`, `softnet_stat` time_squeeze ↑, retransmits ≈ 0 | **Host CPU** (packet processing) |
| `softnet_stat` dropped ↑, `ethtool -S` rx_missed / no_buffer ↑ | **Host NIC / ring buffer** |
| Retransmits ↑, UDP loss ↑, CPU has headroom, **an idle probe pair sharing the loaded link sees its RTT rise** | **Network congestion** (queues in the fabric). If only the loaded host's own RTT rises, the queue is in its own qdisc/socket buffer |
| Out-of-order / SACK-reorder / DSACK ↑, worse with more paths | **Reordering** (ECMP / per-packet spraying) |
| Only one 5-tuple group or one leaf pair is bad | **One path / link** |
| `ip -s link` errors, `ethtool -S` `*err*` counters ↑, TCP resets ↑ | **Err** (the board item): link or driver errors |

> Caveat: in a **virtual** network the switches run on the same physical CPUs as the hosts.
> In that case "network bottleneck" and "CPU bottleneck" can be the same thing. Always record the hypervisor load too.

## 3. Stack

| Role | Tool | Why |
|---|---|---|
| TCP/UDP throughput, loss, jitter, out-of-order | **iperf3 ≥ 3.16** (`-P N`, `-u`, `-J`), **iperf2** (`--trip-times`, `-e`) | UDP reports loss **and** out-of-order directly. **iperf3 before 3.16 runs all streams in one thread**, which by itself produces the "more streams = slower" effect. Use ≥ 3.16 or N separate processes pinned to separate cores. `-J` reports the tool's own CPU on both ends. iperf2 one-way latency needs synced clocks (chrony/PTP, `ptp_kvm` in VMs) |
| Latency / RPC (request-response) | **sockperf**, **netperf TCP_RR** | p50/p99/p99.9, not just averages |
| HPC / MPI collectives | **OSU Micro-Benchmarks** (from the MVAPICH project): `osu_latency`, `osu_bw`, `osu_alltoall`, `osu_allreduce` | Standard MPI pt2pt and collective latency/bandwidth. MVAPICH itself targets RDMA (IB/RoCE), which a virtual network does not have, so build OMB and GPCNeT against **Open MPI or MPICH over TCP**. Optional RDMA path: Soft-RoCE (`rdma_rxe`) |
| Congestion / heterogeneous load | **GPCNeT** | Built for this: a "victim" workload is measured alone and next to "aggressor" traffic (incast, all-to-all). The result is a congestion impact factor |
| Storage | **fio** over NFS / Ceph RBD | Storage-style E-W traffic, large reads/writes |
| Cloud-native / N-S | **wrk2** or **fortio** (HTTP/gRPC) | Many short flows, tail latency |
| Host counters | `nstat`, `ss -ti`, `ethtool -S`, `/proc/net/softnet_stat`, `mpstat -P ALL`, `sar -n DEV,EDEV,ETCP` | Retransmits, reordering, drops, per-core softirq |
| Drop / retransmit tracing | **bcc / bpftrace**: `tcpretrans`, `tcpdrop`; `perf` for CPU profiling | Shows *where* in the kernel packets die and where CPU time goes |
| Packet-level evidence | `tcpdump` + `tshark` (`tcp.analysis.out_of_order`, `retransmission`) | Proof for the report |
| Fault injection (calibration) | `tc netem`, `tc tbf` | Gives known ground truth |
| Orchestration / report | `netbench` (Go): snapshot counters → run load + prober → snapshot again → diff → verdict → JSON/CSV | Skip Prometheus/Grafana until we need live dashboards; plots from CSV with any tool |

## 4. Test matrix (short)

| # | Workload | Pattern | Tool | Key metrics | Expected / pass |
|---|---|---|---|---|---|
| T1 | Baseline, all pairs | E-W, 1 flow | iperf3, ping | Gbit/s, RTT | Close to line rate; same-leaf RTT < cross-spine RTT |
| T2 | Bulk transfer / storage | E-W, bandwidth | iperf3 `-P 1..64`, fio | Throughput, retrans, CPU, softnet | Flat or rising curve; if it drops, the rule names the cause |
| T3 | Latency-sensitive RPC | E-W, small messages | sockperf, netperf TCP_RR | p99/p99.9 RTT | Stable tail latency |
| T4 | HPC/AI collectives | E-W, all-to-all | OSU `alltoall`/`allreduce` | Latency vs message size | Scales smoothly with the number of ranks |
| T5 | Incast | N → 1 | iperf3 multi-client, GPCNeT incast | Loss, retrans, p99 | Shows buffer limits of the leaf |
| T6 | ECMP / load balancing | Many 5-tuples | iperf3 with different ports, UDP | Per-flow Gbit/s, out-of-order | Balanced flows, reordering ≈ 0 |
| T7 | Mixed load | Bulk + RPC at once | iperf3 + sockperf, GPCNeT | Victim p99 alone vs with aggressor | Small slowdown = good isolation |
| T8 | N-S | Host ↔ Internet (BYOD/mobile style) | wrk2/fortio, iperf3 | RPS, p99, loss | Uplink limits are visible |
| T9 | Failure recovery | Link/spine down during load | iperf3 `-i 0.1` + ping | Recovery time, loss during failover | Converges in X ms (ask what the network supports) |

## 5. Priorities

1. **Agent image + collector + verdict, checked with `tc netem`.** Without this, everything else repeats the AKS project mistake.
2. **E-W: T1 + T2 parallelism sweep** with netbench probes under load and an automatic verdict. This is the main demo.
3. **E-W: T6 ECMP / reordering** (per-source-port probes), the SeqErr point from the board.
4. **E-W: T5 incast + T7 mixed load** (bulk aggressor + RPC victim), the heterogeneous-workloads part of the task.
5. **N-S: T8** (many short client connections through the uplink).
6. **T9 failover**, then optionally OSU/GPCNeT over TCP MPI.

## 6. Facts vs assumptions

**Documented:**
- DC traffic characteristics: Benson et al., IMC 2010; Roy et al. (Facebook), SIGCOMM 2015. Roy et al. report less rack locality than earlier studies, so traffic patterns depend on the workload. The "most DC traffic stays inside the DC" figure is usually cited from the Cisco Global Cloud Index.
- Partition-aggregate incast and its effect on tail latency: Alizadeh et al. (DCTCP), SIGCOMM 2010.
- Microbursts: Zhang et al., "High-Resolution Measurement of Data Center Microbursts", IMC 2017. More than 70 % of bursts last at most tens of µs, so coarse counters miss them.
- GPCNeT victim/aggressor methodology: Chunduri et al., SC19.
- DC benchmarking (buffering, microburst, incast, head-of-line blocking): **RFC 8238 / RFC 8239**. These are the closest standards to this task.
- Metric definitions: RFC 2544 (benchmarking), RFC 6349 (TCP throughput), RFC 4737 (reordering), RFC 7679/7680 (one-way delay/loss), RFC 3393 (jitter).

**Assumptions / to confirm with the supervisor:**
- How the virtual network is built (containerlab / GNS3 / SONiC-VS?) and whether we can read switch counters. If we can, it is a big shortcut.
- Whether ECMP is per-flow or per-packet (this decides whether reordering is even expected).
- Host size: "24 × 32" from the voice note is read here as 2 nodes × 24–32 processes.
- Whether the network supports link-down events for T9.
- The decision-rule thresholds are a starting point until netem calibration confirms them.

## 7. Decisions

- **The tool will be used by the lecturer on real networks**, not only on our testbed. So from day one we need: deployment over SSH or a `.deb`, clear CLI help and docs, no dependency on Docker, and a one-way delay that is marked "invalid" when clocks are not synced (we fall back to RTT).
- **x86_64 first.** arm64 later: Go cross-compiles it, but we only test it when there is time.
- **Language: Go.** The lecturer only *uses* the tool, so for them Go and Rust are the same (one binary, `.deb`). Go wins on our development speed: a working tool sooner, and simpler concurrency for the prober, agent and SSH orchestration. Its performance is enough, because line-rate load is iperf3's job.

## 8. Open questions

1. What counts as "done" for the demo: a CLI report, plots, or both?
