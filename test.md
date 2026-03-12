# 🧪 Terminal-Based Algorithm Testing Guide (Windows/PowerShell)

This guide provides the exact commands to verify the five key distributed algorithms. Commands are optimized for **Windows PowerShell**.

## 🚀 1. Setup Environment
Choose **ONE** way to run your cluster:

### Option A: Docker Cluster (Containerized)
```powershell
# Start infrastructure and nodes in background
docker compose up -d
```

### Option B: Native Cluster (Local development)
```powershell
# Run this in the 'frontend' directory
npm run dev
```
*Note: If using Native Cluster, logs will appear directly in that terminal window. Use a SECOND terminal for the curl/control commands below.*

---

## 🏗️ 1. Leader Election (ZooKeeper Ephemeral Sequential Nodes)
**Goal:** Verify one node is elected Master and failover works.

### Check Current Leader (Logs)
```powershell
# For Docker:
docker compose logs | Select-String "is now the leader", "set /leader"

# For Native:
# Watch the terminal where 'npm run dev' is running for "🏆 Node master is statically assigned as MASTER"
```

### Test via API
```powershell
curl http://localhost:8080/leader
```

### Test Failover (Docker Only)
```powershell
# 1. Stop the current leader
docker stop node-master

# 2. Watch for a new node to take over in logs
docker compose logs -f | Select-String "set /leader"
```

---

## ⚖️ 2. Hybrid Task Scheduling (Least Loaded + Round Robin)
**Goal:** Verify tasks are distributed based on load and then round-robin.

### Submit Multiple Tasks
```powershell
# Submit 3 tasks to the cluster
curl -X POST http://localhost:8080/tasks -H "Content-Type: application/json" -d '{\"name\": \"Task A\", \"task_type\": \"batch_processing\", \"priority\": \"high\"}'
curl -X POST http://localhost:8080/tasks -H "Content-Type: application/json" -d '{\"name\": \"Task B\", \"task_type\": \"ai_job\", \"priority\": \"medium\"}'
curl -X POST http://localhost:8080/tasks -H "Content-Type: application/json" -d '{\"name\": \"Task C\", \"task_type\": \"email_notification\", \"priority\": \"low\"}'
```

### Verify Distributions (Logs)
```powershell
# For Docker:
docker compose logs | Select-String "assigned task"

# For Native:
# Watch terminal for "Leader master: assigned task X to worker Y"
```

---

## 💓 3. Heartbeat Failure Detection
**Goal:** Verify the system detects node crashes via heartbeat timeouts (15s).

### Trigger Failure
```powershell
# For Docker:
docker stop node-slave1

# For Native:
# Kill one of the 'api-server.exe' processes in Task Manager OR stop the cluster and restart with fewer nodes.
```

### Verify Detection
```powershell
# Check logs for failure message
docker compose logs | Select-String "Heartbeat failure detected"

# Check worker status via API
curl http://localhost:8080/workers
# Look for "heartbeat": "failed"
```

---

## 📸 4. Chandy–Lamport Distributed Snapshot
**Goal:** Capture a consistent global state.

### Trigger Snapshot
```powershell
# Capture state of tasks, workers, and metadata
curl http://localhost:8080/snapshot
```
*The output is a single JSON object representating the cluster state at that exact millisecond.*

---

## 🔒 5. Ricart–Agrawala Mutual Exclusion
**Goal:** Verify exclusive access to critical sections (CS).

### Trigger CS Request
Operations that mutate sensitive state trigger the algorithm.
```powershell
# Add a worker (Master only)
curl -X POST http://localhost:8080/workers/add

# Delete a task (Master only)
curl -X DELETE http://localhost:8080/tasks/1
```

### Verify Mutual Exclusion (Logs)
```powershell
# Look for request, enter, and release logs
docker compose logs | Select-String "requesting critical section", "entered critical section", "released critical section"
```

---

## 🧹 Cleanup
```powershell
# Clear Docker environment
docker compose down -v

# Stop Native cluster
# Press Ctrl+C in the 'npm run dev' terminal
```
