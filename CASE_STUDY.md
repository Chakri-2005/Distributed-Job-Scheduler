# Distributed Job Scheduler — Case Study Document

## 📌 1. Problem Understanding

### 1.1 Problem Statement

A **Distributed Job Scheduler** schedules, assigns, and executes tasks across multiple worker nodes with automatic leader election, fault tolerance, and coordination — all without single points of failure.

### 1.2 Why This Is a Distributed Systems Problem

| Challenge | Why It Requires DS | Our Solution |
|-----------|-------------------|--------------|
| **Leader Election** | Multiple nodes must agree on one master without centralized authority | ZooKeeper ephemeral sequential znodes |
| **Fault Tolerance** | Master failure must not halt the system | Automatic re-election via chain-watch pattern |
| **Coordination** | Tasks must be assigned to exactly one worker | ZK znodes as distributed locks |
| **Duplicate Execution** | Multiple workers must not execute the same task | Assignment znode + deletion after execution |
| **Consistency Under Failures** | Task state must remain consistent during node crashes | ZooKeeper (CP) + PostgreSQL persistent state |

### 1.3 CAP Theorem Analysis

```
                    CAP Theorem
                    ┌─────────┐
                    │    C    │  ← Consistency
                   / \       │
                  /   \      │
                 /     \     │
           ZooKeeper    \    │
             (CP)        \   │
              │           \  │
              │            \ │
              A ────────────P
         Availability    Partition Tolerance
```

**ZooKeeper is CP (Consistent + Partition-tolerant):**

- **Consistency**: All nodes see the same data at the same time. ZK uses ZAB (ZooKeeper Atomic Broadcast) protocol for linearizable writes.
- **Partition tolerance**: System continues operating despite network partitions between ZK nodes.
- **Availability trade-off**: During a network partition, if a ZK quorum cannot be reached, the minority partition becomes unavailable (refuses writes). This guarantees consistency.

**Why CP is critical for a job scheduler:**
- Leader election MUST be consistent — two leaders would cause duplicate task execution.
- Task assignment MUST be atomic — partial assignments would cause data corruption.
- Better to temporarily stop scheduling than to run duplicate tasks.

### 1.4 Why ZooKeeper Instead of a Normal Database?

| Feature | PostgreSQL (DB) | ZooKeeper |
|---------|----------------|-----------|
| Ephemeral nodes (auto-deleted on crash) | ❌ | ✅ |
| Watch/event notification | ❌ (needs polling) | ✅ |
| Sequential ordering guarantees | ❌ | ✅ |
| Leader election primitive | Manual + complex | Built-in via ephemeral sequential |
| Session management | Manual heartbeat | Automatic session + timeout |
| Low-latency coordination | Slow (disk-based) | Fast (in-memory tree) |

**Key insight**: ZooKeeper provides primitives (ephemeral nodes, watches, sequential ordering) that make distributed coordination simple and correct. A normal DB would require complex custom logic for all of these.

---

## 🏗️ 2. Architecture & Algorithms

### 2.1 System Architecture

```
┌────────────────────────────────────────────────────────────────────┐
│                         Docker Network                            │
│                                                                    │
│  ┌──────────┐    REST    ┌──────────────┐         ┌────────────┐  │
│  │ Frontend │◄──────────►│  API Server  │◄───────►│ ZooKeeper  │  │
│  │ React+TS │   :3000    │  Go + Gin    │         │   :2181    │  │
│  │  (Vite)  │            │    :8080     │         │   (CP)     │  │
│  └──────────┘            └──────┬───────┘         └─────┬──────┘  │
│                                 │                       │         │
│                            ┌────▼────┐            ┌─────▼─────┐  │
│                            │PostgreSQL│            │  ZNode    │  │
│                            │  :5432   │            │  Tree     │  │
│                            │(persist) │            │ (coord)   │  │
│                            └─────────┘            └─────┬─────┘  │
│                                                         │         │
│                    ┌────────────┬──────────┬─────────────┘         │
│                    │            │          │                       │
│              ┌─────▼────┐ ┌────▼────┐ ┌───▼─────┐                │
│              │ Worker-1 │ │Worker-2 │ │Worker-3 │                │
│              │ (LEADER) │ │(FOLLOWER│ │(FOLLOWER│                │
│              └──────────┘ └─────────┘ └─────────┘                │
└────────────────────────────────────────────────────────────────────┘
```

### 2.2 ZNode Hierarchy

```
/ (ZooKeeper root)
├── /workers/                          ← Worker registration
│   ├── worker-1_0000000001           (ephemeral, sequential)
│   ├── worker-2_0000000002           (ephemeral, sequential)
│   └── worker-3_0000000003           (ephemeral, sequential)
│
├── /tasks/                            ← Pending task queue
│   └── task_42                       (created by API, deleted after assignment)
│
├── /assignments/                      ← Task assignments per worker
│   ├── worker-1_0000000001/
│   │   └── task_42                   (created by leader, deleted after execution)
│   ├── worker-2_0000000002/
│   └── worker-3_0000000003/
│
└── /leader                            ← Current leader node ID
    (data: "worker-1_0000000001")
```

### 2.3 Algorithm 1: Leader Election (Ephemeral Sequential Nodes)

**Mechanism**: Each worker creates an ephemeral sequential znode. The worker with the smallest sequence number becomes leader. Others watch their predecessor (chain-watch pattern).

```
ALGORITHM LeaderElection:

1. Worker connects to ZooKeeper
2. CREATE /workers/{workerID}_ (Ephemeral + Sequential)
   → ZK returns: /workers/worker-1_0000000005

3. GET CHILDREN of /workers
   → [worker-1_0000000005, worker-2_0000000003, worker-3_0000000007]

4. SORT children by sequence number
   → [worker-2_0000000003, worker-1_0000000005, worker-3_0000000007]

5. IF my_node == children[0]:
     I AM THE LEADER
     SET /leader = my_node_name
     START watching /tasks for assignment
   ELSE:
     predecessor = children[my_position - 1]
     WATCH predecessor (ExistsW)
     WAIT for watch event (predecessor deleted)
     GO TO step 3 (re-run election)

FAILOVER:
  When leader crashes → ephemeral node auto-deleted
  → predecessor watcher fires on next node
  → that node re-runs election
  → becomes new leader (smallest remaining sequence)
```

**Why chain-watch?** If all followers watched the leader node, a "herd effect" would occur where all followers wake up simultaneously. Chain-watch ensures only one follower (the next in sequence) reacts.

### 2.4 Algorithm 2: Distributed Lock / Task Assignment

**Mechanism**: ZooKeeper znodes act as distributed locks. Only the leader assigns tasks, and each task's assignment znode prevents duplicate execution.

```
ALGORITHM TaskAssignment (Leader only):

1. WATCH /tasks for children changes (ChildrenW)

2. ON new task detected (/tasks/task_N):
   a. READ task data from /tasks/task_N
   b. GET CHILDREN of /workers → list of active workers
   c. For each worker, COUNT children of /assignments/{worker}
   d. SELECT worker with MINIMUM assignments (load balancing)

3. CREATE /assignments/{selected_worker}/task_N
   → This is the DISTRIBUTED LOCK: only one worker gets this assignment
   → If CREATE fails with NodeExists → another leader already assigned it

4. DELETE /tasks/task_N
   → Prevents re-assignment

WORKER EXECUTION:
  1. Each worker WATCHES /assignments/{my_node}/ (ChildrenW)
  2. ON new assignment: READ task_id from znode
  3. EXECUTE task based on task_type
  4. UPDATE DB status → completed
  5. DELETE /assignments/{my_node}/task_N → releases the lock
```

### 2.5 Algorithm 3: Watch-Based Event Triggering

```
ALGORITHM WatchEventCoordination:

Three independent watch chains operate simultaneously:

Chain 1: Leader Election
  Each worker watches predecessor → triggers re-election on failure

Chain 2: Task Detection  (Leader → /tasks)
  Leader watches /tasks children → triggers assignment when new task created

Chain 3: Task Execution  (Worker → /assignments/{worker})
  Each worker watches its assignment directory → triggers execution on new assignment

                    API creates task
                         │
                         ▼
                   /tasks/task_42  ──────── Watch fires on Leader
                                                    │
                                                    ▼
                                           Leader assigns task
                                                    │
                                                    ▼
                                   /assignments/worker-1/task_42
                                           ──── Watch fires on Worker-1
                                                    │
                                                    ▼
                                           Worker-1 executes task
                                                    │
                                                    ▼
                                           DELETE assignment znode
                                           UPDATE DB → completed
```

---

## 🖥️ 3. Implementation (Multi-Machine)

### 3.1 Services Running

| Service | Build | Container | Port |
|---------|-------|-----------|------|
| ZooKeeper | `confluentinc/cp-zookeeper:7.5.0` | `zookeeper` | 2181 |
| PostgreSQL | `postgres:15-alpine` | `postgres` | 5432 |
| API Server | Go + Gin (custom Dockerfile) | `api-server` | 8080 |
| Worker 1 | Go (custom Dockerfile) | `worker-1` | — |
| Worker 2 | Go (custom Dockerfile) | `worker-2` | — |
| Worker 3 | Go (custom Dockerfile) | `worker-3` | — |
| Frontend | React + Nginx | `frontend` | 3000 |

**Total: 7 containers running simultaneously.**

### 3.2 How to Reproduce

```bash
# 1. Start all services
docker-compose up --build

# 2. Open dashboard
# http://localhost:3000

# 3. Create tasks via UI or API
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -d '{"name": "Process CSV", "task_type": "batch_processing"}'

curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -d '{"name": "Alert Users", "task_type": "email_notification"}'

curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -d '{"name": "Train Model", "task_type": "ai_job"}'
```

### 3.3 Failure Simulation

```bash
# Find current leader (check dashboard or):
curl http://localhost:8080/leader

# Kill the master worker
docker-compose stop worker-1

# Watch logs — re-election happens in seconds:
docker-compose logs -f worker-2 worker-3

# Expected output:
# worker-2: predecessor gone (event: EventNodeDeleted), re-running election...
# worker-2: 🏆 Worker worker-2 is now MASTER/LEADER!
# worker-2: set /leader = worker-2_0000000004

# Restart worker — it rejoins as follower
docker-compose start worker-1
```

### 3.4 Retry Mechanism

When a task fails (~10% simulated failure rate):
1. Worker marks task as `failed` in DB
2. Scheduler detects `failed` tasks with `retry_count < max_retries`
3. Scheduler resets task to `pending`, increments `retry_count`
4. Task is re-assigned to a worker (possibly different one)
5. Dashboard shows retry count badge (e.g., `1/3`)

---

## 📊 4. Output — Three Use Cases

### Use Case 1: Batch File Processing 📦

**Scenario**: Process a large CSV dataset in chunks.

| Step | Action | System Behavior |
|------|--------|----------------|
| 1 | User selects "Batch Processing" type | Task type = `batch_processing` |
| 2 | Creates task "Process CSV Data" | API creates DB record + `/tasks/task_N` znode |
| 3 | Leader detects new task | Watch on `/tasks` fires |
| 4 | Leader assigns to worker with fewest tasks | Creates `/assignments/worker-X/task_N` |
| 5 | Worker executes in 3-5 steps | Logs: "Processing record 50/200 (25%)" |
| 6 | Completion | Status → `completed`, assignment deleted |

### Use Case 2: Email Notification System 📧

**Scenario**: Send alert emails to multiple recipients.

| Step | Action | System Behavior |
|------|--------|----------------|
| 1 | User selects "Email Notification" type | Task type = `email_notification` |
| 2 | Creates task "Alert Admins" | DB record + znode created |
| 3 | Leader assigns task | Load-balanced across workers |
| 4 | Worker sends emails one-by-one | Logs: "Email sent to recipient 3/8" |
| 5 | All emails delivered | Logs: "All 8 emails delivered successfully" |
| 6 | Completion (fast: 2-3 seconds) | Status → `completed` |

### Use Case 3: AI Job Scheduling 🤖

**Scenario**: Train a machine learning model with multiple epochs.

| Step | Action | System Behavior |
|------|--------|----------------|
| 1 | User selects "AI Job" type | Task type = `ai_job` |
| 2 | Creates task "Train Classifier" | DB record + znode created |
| 3 | Leader assigns task | Typically to least-loaded worker |
| 4 | Training phase | Logs: "Epoch 2/5 — loss: 0.4523, accuracy: 72.3%" |
| 5 | Inference phase | Logs: "Running inference on test dataset..." |
| 6 | Completion (long: 8-12 seconds) | Logs: "Inference complete — accuracy: 91.4%" |

### Dashboard Features

The dashboard shows all three use cases simultaneously:
- **Task type badges**: 📦 Batch, 📧 Email, 🤖 AI Job
- **Type-specific filters**: Filter tasks by type
- **Execution logs**: Click 📜 to see step-by-step execution logs per task
- **System events**: Leader election, worker joins, failover events in real-time
- **Stats breakdown**: Pie chart by status + bar chart by task type
- **Retry tracking**: Failed tasks show retry count badge

---

## 🎤 5. Presentation Script & Viva Q&A

### 10-Minute Presentation Script

**Slide 1 (1 min) — Introduction**
"Our project is a Distributed Job Scheduler built with Go, React, ZooKeeper, and PostgreSQL. It demonstrates key distributed systems concepts: leader election, fault tolerance, and distributed coordination."

**Slide 2 (1.5 min) — Problem Statement & DS Relevance**
"Scheduling jobs across multiple machines requires solving fundamental DS problems: How do we elect a leader? How do we handle failures? How do we prevent duplicate execution? These are exactly the problems that distributed systems theory addresses."

**Slide 3 (1 min) — CAP Theorem**
"We chose ZooKeeper because it's CP — consistent and partition-tolerant. For a job scheduler, consistency is critical. Two leaders would cause duplicate task execution. ZooKeeper guarantees that all nodes agree on who the leader is, even during network partitions."

**Slide 4 (2 min) — Architecture**
"Our system has 7 Docker containers. The API server receives tasks. ZooKeeper coordinates the workers. PostgreSQL stores task history. Three worker nodes compete for leadership using ephemeral sequential znodes."

**Slide 5 (2 min) — Leader Election Algorithm**
"Each worker creates an ephemeral sequential znode. The smallest sequence number becomes leader. Others watch their predecessor — this is the chain-watch pattern that avoids the herd effect. When the leader crashes, its ephemeral node auto-deletes, triggering re-election."

**Slide 6 (1 min) — Task Assignment Algorithm**
"The leader watches /tasks for new entries. It assigns each task to the worker with the fewest current assignments, creating a distributed lock via ZK znodes. After execution, the worker deletes the assignment to release the lock."

**Slide 7 (1.5 min) — Live Demo**
"Let me show you. I'll create three tasks — batch processing, email notification, and AI training. Watch how they get assigned to different workers. Now I'll kill the leader... and you can see re-election happening in seconds."

### Viva Questions & Answers

**Q1: Why did you choose ZooKeeper?**
A: ZooKeeper provides ephemeral nodes (auto-deleted on crash), sequential ordering (for deterministic leader election), and watches (for event-driven coordination). These primitives make distributed coordination simple and correct. A normal database would require complex custom logic for all of these.

**Q2: Why is ZooKeeper CP and not AP?**
A: ZooKeeper uses the ZAB (ZooKeeper Atomic Broadcast) protocol. During a network partition, if a quorum cannot be reached, the minority side refuses writes to maintain consistency. This means availability is sacrificed, but we never get split-brain (two leaders).

**Q3: Why use ephemeral nodes?**
A: Ephemeral nodes are automatically deleted when the creating client's session expires (e.g., on crash). This is crucial for leader election — when a leader crashes, its znode disappears, triggering automatic re-election without any manual intervention.

**Q4: How does failover work?**
A: Each follower watches its predecessor node (chain-watch pattern). When the leader crashes: (1) its ephemeral node is auto-deleted, (2) the next node's watcher fires, (3) that node re-runs election, (4) if it's now the smallest sequence, it becomes leader, (5) it writes its ID to /leader and starts assigning tasks. This takes 2-5 seconds.

**Q5: How do you prevent duplicate task execution?**
A: Three mechanisms: (1) The leader creates an assignment znode under a specific worker — only one worker sees it. (2) The task is deleted from /tasks after assignment. (3) The worker deletes its assignment znode after completing the task. (4) DB status is updated atomically with WHERE status='pending' clause.

**Q6: What happens if a worker crashes mid-task?**
A: The task remains in 'running' status. The scheduler's retry mechanism detects tasks that have been running too long or are marked as failed, and re-queues them (up to max_retries). The ephemeral assignment node is deleted when the worker's session expires.

**Q7: What is the chain-watch pattern?**
A: Instead of all followers watching the leader node (which causes "herd effect" — all nodes wake up simultaneously), each follower watches only its predecessor in the sequence. This means only one node wakes up when the leader fails, reducing ZooKeeper load.

**Q8: How does load balancing work?**
A: The leader counts current assignments per worker and assigns new tasks to the worker with the fewest active assignments. This ensures even distribution across all worker nodes.

**Q9: Why not use a message queue like RabbitMQ?**
A: A message queue handles task distribution but not leader election or coordination. ZooKeeper provides both coordination and leader election primitives. We use PostgreSQL for persistent task history, giving us the best of both worlds.

**Q10: How is this different from Kubernetes Jobs?**
A: Kubernetes Jobs use etcd (similar to ZK) internally. Our project demonstrates the same underlying concepts (leader election, coordination) at a lower level, making the distributed systems concepts explicit and visible.

**Q11: What are the three use cases?**
A: (1) Batch File Processing — processes records in chunks with progress tracking. (2) Email Notification — sends emails to multiple recipients sequentially. (3) AI Job Scheduling — simulates ML model training with epochs, loss tracking, and inference.

**Q12: How does the retry mechanism work?**
A: When a task fails, the scheduler detects it (status='failed' AND retry_count < max_retries), increments retry_count, resets status to 'pending', and re-creates the task znode. This triggers re-assignment to a potentially different worker.

**Q13: What is the ZAB protocol?**
A: ZooKeeper Atomic Broadcast is ZooKeeper's consensus protocol (similar to Raft/Paxos). It ensures that all ZK servers agree on the order of state changes, providing linearizable writes and sequential consistency.

**Q14: What if ZooKeeper itself goes down?**
A: In a production setup, ZooKeeper runs as a cluster (typically 3 or 5 nodes). It tolerates failure of up to N/2 - 1 nodes. In our demo, we use a single ZK node for simplicity, but the code supports multi-node ZK clusters.

**Q15: How does the frontend get real-time updates?**
A: The frontend polls the API every 2 seconds. The API reads live data from both ZooKeeper (workers, leader, assignments) and PostgreSQL (tasks, events, stats), providing a near-real-time view of the cluster state.
