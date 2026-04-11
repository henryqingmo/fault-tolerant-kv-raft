# raft

A Go implementation of the [Raft](https://raft.github.io/) distributed consensus algorithm. Raft ensures a cluster of servers maintains an identical, fault-tolerant replicated log — the foundation for building strongly consistent distributed systems.

## Overview

This library implements the core Raft protocol:

- **Leader election** — randomized election timeouts, majority quorum, automatic re-election on leader failure
- **Log replication** — ordered append-only log replicated across all cluster members
- **Safety** — only entries committed in the current term advance the commit index, preventing stale-leader divergence

The network layer (`labrpc`) is a channel-based RPC implementation that supports controlled packet loss, arbitrary message delays, and dynamic node disconnection — enabling tests that would be brittle or impossible against a real network.

## Architecture

```
src/
├── raft/        # Core Raft state machine
│   ├── raft.go  # Leader election, log replication, apply loop
│   └── util.go  # Debug logging
├── labrpc/      # Simulated RPC transport
└── labgob/      # Gob encoding with safety checks
```

Each `Raft` instance runs three background goroutines:

| Goroutine | Role |
|-----------|------|
| Election timer | Detects heartbeat loss and starts elections |
| Heartbeat/replication loop | Leader sends `AppendEntries` every 100ms |
| Apply loop | Delivers committed entries to the service layer via `applyCh` |

## RPC Interface

**RequestVote** — issued by candidates during elections

```go
Args:  { Term, CandidateId, LastLogIndex, LastLogTerm }
Reply: { Term, VoteGranted }
```

**AppendEntries** — used for both heartbeats and log replication

```go
Args:  { Term, LeaderId, PrevLogIndex, PrevLogTerm, Entries[], LeaderCommit }
Reply: { Term, Success }
```

## Usage

```go
peers   := // []*labrpc.ClientEnd — RPC endpoints for each peer
persister := raft.MakePersister()
applyCh   := make(chan raft.ApplyMsg)

rf := raft.Make(peers, me, persister, applyCh)

// Propose a command (leader only)
index, term, isLeader := rf.Start(command)

// Committed entries are delivered on applyCh
msg := <-applyCh
// msg.Command, msg.CommandIndex
```

## Configuration

| Parameter | Value |
|-----------|-------|
| Election timeout | 150–300ms (randomized) |
| Heartbeat interval | 100ms |
| Apply loop frequency | 10ms |
| Quorum | ⌊N/2⌋ + 1 |

## Testing

Tests run against `labrpc`'s simulated network, which can inject failures that would be non-deterministic on a real network.

```bash
cd src/raft
go test ./...
```

```bash
# Run a specific test suite
go test -run TestInitialElection2A -v
go test -run TestBackup2B -v
```

**Leader election tests**

- `TestInitialElection2A` — stable cluster elects exactly one leader
- `TestReElection2A` — new election on leader failure; no election without quorum

**Log replication tests**

- `TestBasicAgree2B` — entries replicated to all nodes
- `TestFailAgree2B` — cluster makes progress with one failed follower
- `TestFailNoAgree2B` — no commit without majority
- `TestConcurrentStarts2B` — concurrent proposals from a single leader
- `TestRejoin2B` — partitioned leader safely rejoins and replays log
- `TestBackup2B` — followers catch up after significant log divergence
- `TestCount2B` — RPC efficiency under normal operation

## Key Implementation Notes

- All shared state is protected by a single mutex (`rf.mu`); goroutines acquire it before reading or writing any Raft fields.
- An entry at index 0 (`{nil, term: 0}`) acts as a sentinel, simplifying boundary conditions in `PrevLogIndex` checks.
- Only entries from the **current term** advance `commitIndex` (Raft §5.4.2), preventing the "leader completeness" violation.
- On a log conflict, the follower truncates from the first mismatched index; the leader backs `nextIndex` down and retries.

## References

- Ongaro & Ousterhout, [*In Search of an Understandable Consensus Algorithm*](https://raft.github.io/raft.pdf) (USENIX ATC 2014)
- [The Raft website](https://raft.github.io/) — visualization and formal specification

## License

MIT
