# Data Center Network Workloads

A utility that runs data center workloads on a black-box network, collects host and network
metrics in parallel, and tells whether the network is the bottleneck and which network problem
(loss, latency, errors, link congestion) causes the drop.

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

1. **Main goal:** a utility that runs workloads, collects host and network metrics at the same time, and tells whether the network is the bottleneck and which network problem causes the drop.
2. Workload table: traffic type (East-West / North-South; bandwidth-, latency-oriented or mixed) and network requirements.
3. Test matrix (scenarios for the utility): workload, tool, metrics, expected result.
4. Use the utility to study how workloads affect each other when run together.
5. Test on the provided network and recommend test priorities.

## Utility

Three parts:

1. **Scenario runner**: starts load generators from a test-matrix scenario, stepping up the number of threads and nodes.
2. **Metric collection**: host agents capture CPU (mpstat, sar) and network (ss, nstat, interface counters, active latency/loss probes) metrics while the load runs.
3. **Analysis and report**: compares against a no-load baseline and correlates metrics. If performance drops while CPU is not saturated and latency, loss, SeqErr or retransmits grow, it names the network as the cause and the specific symptom (rules: [docs/bottleneck-diagnosis.md](docs/bottleneck-diagnosis.md)). Comparing paths within one access switch vs through the spine locates the bottleneck layer.

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
| mpstat, sar, ss, nstat, interface counters, active probes | CPU and network state during a test (collected by the utility) |

**Metrics:** throughput, latency (mean and p99), Loss, Err, SeqErr, retransmits, CPU utilization.

## Deliverables

- [ ] **Utility (main deliverable):** scenario runner, metric collection, automatic bottleneck detection (network vs CPU, specific network symptom), with docs
- [ ] Workload table
- [ ] Test matrix
- [ ] Test results: utility reports for single and mixed workloads on the provided network
- [ ] Prioritized test recommendations (documented facts separated from assumptions)

## Limitations

- A virtual network behaves differently from physical hardware (switch buffers, RDMA), so behavior is compared, not absolute values.
- In a virtual environment the host CPU can itself become the bottleneck; the utility accounts for this in analysis.
- The network configuration is unknown, so the problem layer is inferred indirectly.

## Work split (preliminary)

- Maryna Ohinska: workload table, MPI scenarios (MVAPICH, GPCNeT), analysis and report module.
- Nazar Mykhailyshchuk: storage and TCP scenarios (fio, iperf3), scenario runner and metric collection modules, failure scenarios.
- Together: test matrix, mixed workloads, result analysis, recommendations.

## Project structure

```
cmd/netbench/       # entry point: netbench run / netbench agent
internal/runner/    # scenario runner (iperf3, OSU, GPCNeT, fio), stepping threads and nodes
internal/collect/   # metric collection: mpstat, sar, ss, nstat, interface counters, probes
internal/analyze/   # baseline comparison, verdict, report (docs/bottleneck-diagnosis.md rules)
scenarios/          # YAML scenarios from the test matrix
testbed/            # local testbed (docker), packaging
docs/               # proposal, diagnosis rules
results/            # measurement output (gitignored)
```

## Documentation

- [docs/proposal.md](docs/proposal.md): project proposal (UA)
- [docs/bottleneck-diagnosis.md](docs/bottleneck-diagnosis.md): bottleneck diagnosis rules
