# Data Center Network Workloads

Workload-driven black-box testing of data center networks.

## Authors

| | Name | GitHub |
|:-:|---|---|
| <img src="https://github.com/mohinska.png?size=80" width="48" alt="mohinska"/> | Maryna Ohinska | [@mohinska](https://github.com/mohinska) |
| <img src="https://github.com/partum55.png?size=80" width="48" alt="partum55"/> | Nazar Mykhailyshchuk | [@partum55](https://github.com/partum55) |

## Problem

A data center runs AI/HPC, virtual machines, cloud-native services and distributed storage
at the same time. Model training and storage replication generate server-to-server traffic
(East-West); clients, including BYOD and mobile devices, reach services from outside
(North-South). Some workloads need bandwidth, others low latency.

Load generators do not report network parameters. When a benchmark result drops, it is
unclear whether the cause is CPU or the network: e.g. more threads can lower performance
because of the network, but the benchmark does not show it. The goal is to tell **when**
the network is the cause and **what** it is: loss, latency, errors, or link congestion.

The network is a **black box**: an already configured virtual network, with load run on top of it.

## Goals

1. Workload table: traffic type (East-West / North-South; bandwidth-, latency-oriented or mixed) and network requirements.
2. Test matrix: workload, tool, metrics, expected result.
3. Method to separate network from CPU problems and name the specific network cause.
4. Study how workloads affect each other when run together.
5. Test on the provided network and recommend test priorities.

## Method

1. Measure without load first.
2. Gradually increase the number of threads and nodes while collecting CPU and network metrics in parallel.
3. If performance drops while CPU is not saturated and latency, loss or retransmits grow, the cause is the network.
4. Compare traffic within one access switch vs through the spine to locate the bottleneck layer.

Detailed testing covers AI/HPC and distributed storage (the most network-demanding);
virtualization and cloud-native services are used as background traffic.

**Scenarios:** throughput, latency under load, incast, load balancing across spine links,
link/node failure, and mixed workloads (AI/HPC + storage + background N-S) for performance
isolation, bandwidth sharing under congestion/oversubscription, and recovery after failure.

## Tools and metrics

| Tool | Purpose |
|---|---|
| MVAPICH (OSU Micro-Benchmarks) | Latency, bandwidth, all-reduce / all-to-all as an AI/HPC model |
| GPCNeT | Impact of congestion on other traffic |
| iperf3 / netperf | Baseline throughput and latency, TCP/UDP, varying stream count |
| fio over network storage | Storage traffic, incast |
| mpstat, sar, ss, nstat, ping | CPU and network state during a test |

**Metrics:** throughput, latency (mean and p99), Loss, Err, SeqErr, retransmits, CPU utilization.

## Deliverables

- [ ] Workload table
- [ ] Test matrix
- [ ] Network-vs-CPU method, demonstrated on real measurements
- [ ] Test results for single and mixed workloads, with bottleneck location
- [ ] Prioritized test recommendations (documented facts separated from assumptions)

## Limitations

- A virtual network behaves differently from physical hardware (switch buffers, RDMA), so behavior is compared, not absolute values.
- In a virtual environment the host CPU can itself become the bottleneck; the method accounts for this.
- The network configuration is unknown, so the problem layer is inferred indirectly.

## Documentation

- [docs/proposal.md](docs/proposal.md): project proposal (UA)
- [docs/bottleneck-diagnosis.md](docs/bottleneck-diagnosis.md): bottleneck diagnosis rules
