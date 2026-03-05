# Distributed Algorithms Report

This case study project implements five major distributed algorithms to ensure fault tolerance, consensus, coordinated state management, and real-time updates across multiple nodes.

Below is the detailed documentation for each algorithm and its precise integration into the codebase.

---

## 1. Leader Election (ZooKeeper Ephemeral Sequential Nodes)

**Short Definition:**
Leader Election is a distributed algorithm used to select one coordinator node among multiple nodes in a cluster. In this system, it is achieved using ZooKeeper's ephemeral sequential znodes. The node that obtains the znode with the smallest sequence number automatically becomes the leader.

**Why This Algorithm Is Used in This Project:**
Leader election is crucial to ensure that only one node (the Master) acts as the scheduler at any given time. This strictly prevents multiple nodes from assigning the same task simultaneously, avoiding split-brain scenarios and guaranteeing robust consensus.

**Where the Algorithm Is Implemented in the Code:**
- **File:** `api-server/node.go`
- **Function:** `Register()`
- **Lines:** 122–151 (and also `api-server/zk.go` at `AddDynamicWorkerZNode()`)

**Description:**
During registration, each node automatically connects to ZooKeeper and creates a node inside the `/workers` path using the `zk.FlagEphemeral | zk.FlagSequence` flags. The ephemeral guarantee means if the node crashes, its znode instantly vanishes.

**Key Code Snippet:**
```go
// Creates ephemeral sequential node under /workers for election
nodePath := fmt.Sprintf("/workers/%s_", cn.NodeID)
created, err := cn.ZKClient.conn.Create(
    nodePath,
    []byte(cn.NodeID),
    zk.FlagEphemeral|zk.FlagSequence,
    zk.WorldACL(zk.PermAll),
)
```

---

## 2. Hybrid Task Scheduling (Least Loaded + Round Robin)

**Short Definition:**
This hybrid algorithm allocates incoming tasks to active workers. It primarily uses "Least Loaded" to assign tasks to the worker with the fewest running jobs. If multiple workers share the exact same load, it elegantly falls back to a Round-Robin approach to distribute tasks evenly among them.

**Why This Algorithm Is Used in This Project:**
This strategy provides incredibly efficient load balancing. It prevents any single worker from being overwhelmed while avoiding the unpredictability of pure random assignment.

**Where the Algorithm Is Implemented in the Code:**
- **File:** `api-server/node.go`
- **Function:** `pickWorkerHybrid()`
- **Lines:** 452–498

**Description:**
The master evaluates all active workers and their current number of assignments in ZooKeeper. It filters for workers with the absolute minimum count. The secondary round-robin pointer iterates through those ties.

**Key Code Snippet:**
```go
	// Find minimum load
	minLoad := loads[0].count
	var minWorkers []string
	for _, l := range loads {
		if l.count < minLoad {
			minLoad = l.count
			minWorkers = []string{l.name}
		} else if l.count == minLoad {
			minWorkers = append(minWorkers, l.name)
		}
	}
    // Round-robin fallback among equally loaded workers
	idx := *roundRobinIdx % len(minWorkers)
	*roundRobinIdx++
	return minWorkers[idx]
```

---

## 3. Distributed Heartbeat Failure Detection

**Short Definition:**
The heartbeat algorithm requires all active worker nodes to routinely transmit a "keep-alive" signal. If the master hasn't received a signal from a worker within a set threshold (15 seconds), the worker is permanently marked as failed.

**Why This Algorithm Is Used in This Project:**
In a distributed mesh, hard crashes or internet blackouts happen. Without heartbeats, the master would permanently assign tasks to "dead" nodes. This algorithm guarantees fault tolerance so their tasks can be swiftly reassigned.

**Where the Algorithm Is Implemented in the Code:**
- **File:** `api-server/node.go`
- **Function:** `startHeartbeat()` and `watchHeartbeats()`
- **Lines:** 207–246

**Description:**
Every 5 seconds, each node runs `startHeartbeat()` and writes its Unix timestamp into `/heartbeats/<ID>`. The master runs `watchHeartbeats()` every 10 seconds. If `now - lastBeat > 15000ms`, it triggers `handleHeartbeatFailure()`.

**Key Code Snippet:**
```go
    // If more than 15 seconds since last heartbeat, mark failed
    if now-lastBeat > 15000 {
        log.Printf("⚠️ Heartbeat failure detected for worker: %s", workerBase)
        cn.handleHeartbeatFailure(workerBase)
    }
```

---

## 4. Chandy–Lamport Distributed Snapshot

**Short Definition:**
The Chandy-Lamport algorithm is used to capture a globally consistent view (a "snapshot") of a complex distributed system's state without needing to halt ongoing executions across all nodes. 

**Why This Algorithm Is Used in This Project:**
The scheduler has constantly moving parts: jobs are added, execution fails, workers die. Providing a "Snapshot" view guarantees the Master UI has a perfectly aligned, single-point-in-time calculation of active workers and completed work per-worker without pausing the pipeline.

**Where the Algorithm Is Implemented in the Code:**
- **File:** `api-server/node.go`
- **Function:** `GetClusterSnapshot()`
- **Lines:** 701–759

**Description:**
When requested by the `/snapshot` endpoint, the Master fetches the leader ID, active worker registry, ongoing heartbeat mappings, and exact task statuses across all parallel running buckets and compiles them safely.

**Key Code Snippet:**
```go
// GetClusterSnapshot builds a Chandy-Lamport style cluster snapshot
func GetClusterSnapshot(zkClient *ZKClient, db *sql.DB) map[string]interface{} {
	leader, _ := zkClient.GetLeader()
	workers, _ := zkClient.GetWorkers()
	heartbeats := zkClient.GetHeartbeats()
    // Combines data with Database counts internally...
}
```

---

## 5. Ricart–Agrawala Mutual Exclusion

**Short Definition:**
Ricart-Agrawala is a classical distributed mutual exclusion algorithm ensuring that multiple nodes coordinating across a network do not attempt to enter a critical section simultaneously.

**Why This Algorithm Is Used in This Project:**
When the Master executes sensitive network operations like removing a worker, deleting a task, or reassigning loads, it requires absolute exclusivity over those mutating operations. Simulating a lock prevents race conditions in the Database and ZooKeeper trees.

**Where the Algorithm Is Implemented in the Code:**
- **File:** `api-server/node.go`
- **Function:** `MutualExclusionState` (struct and methods)
- **Lines:** 39–89

**Description:**
Before endpoints like `DELETE /tasks` or `POST /workers/add` execute, the route calls `AcquireCS()` (Acquire Critical Section). It locks the resource natively and logs out exactly which node entered the critical section. Upon completion, it broadcasts a `ReleaseCS()` event freeing up the system.

**Key Code Snippet:**
```go
// AcquireCS acquires the simulated critical section
func (m *MutualExclusionState) AcquireCS(reason, nodeID string, db *sql.DB) {
	m.mu.Lock()
	m.inCS = true
	m.timestamp = time.Now().UnixMilli()
	m.mu.Unlock()
	CreateEvent(db, "mutual_exclusion_request",
		nodeID, fmt.Sprintf("%s requesting critical section (clock: %d)", nodeID, m.timestamp))
}
```
