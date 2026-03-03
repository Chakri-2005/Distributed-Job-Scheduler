# Distributed Job Scheduler

A full-stack **Distributed Job Scheduler** demonstrating leader election, fault tolerance, distributed coordination, and automatic failover.

> 📖 **Case Study Document**: See [CASE_STUDY.md](./CASE_STUDY.md) for the complete case study with algorithms, pseudocode, CAP theorem analysis, 3 use cases, presentation script, and viva Q&A.

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Frontend | React + TypeScript + Vite + Recharts |
| API Server | Go + Gin |
| Workers | Go |
| Coordination | Apache ZooKeeper |
| Database | PostgreSQL |
| Infra | Docker + Docker Compose |

## Architecture

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

## Three Use Cases

| Use Case | Type | Execution Time | Description |
|----------|------|---------------|-------------|
| 📦 Batch File Processing | `batch_processing` | 5-8s | Process records in chunks with progress tracking |
| 📧 Email Notification | `email_notification` | 2-3s | Send emails to multiple recipients sequentially |
| 🤖 AI Job Scheduling | `ai_job` | 8-12s | ML model training (epochs + loss) and inference |

## How Leader Election Works

1. Each worker creates an **ephemeral sequential znode**: `/workers/worker-1_0000000001`
2. Workers list siblings and sort by sequence number
3. Worker with the **smallest sequence** becomes MASTER
4. Other workers watch their **predecessor** node (chain-watch pattern)
5. If MASTER crashes → its ephemeral node disappears → predecessor watcher fires → re-election
6. New leader writes its ID to `/leader` znode

## Task Flow

```
UI → POST /tasks → DB (pending) → /tasks/task_N znode
                                          ↓
                              MASTER watches /tasks
                                          ↓
                              /assignments/worker-X/task_N
                                          ↓
                              Worker watches its assignment dir
                                          ↓
                              Worker executes (type-specific simulation)
                                          ↓
                              DB status → completed (or failed → auto-retry)
                              Assignment znode deleted (no duplicates)
```

## Quick Start

### Prerequisites
- Docker Desktop
- Docker Compose

### Start the System

```bash
docker-compose up --build
```

All 7 services will start:
- `zookeeper` — ZooKeeper coordination service
- `postgres` — PostgreSQL database
- `api-server` — REST API on port 8080
- `worker-1`, `worker-2`, `worker-3` — Workers competing for leadership
- `frontend` — React dashboard on port 3000

Open the dashboard: **http://localhost:3000**

### Create Tasks (All 3 Use Cases)

```bash
# Use Case 1: Batch Processing
curl -X POST http://localhost:8080/tasks -H "Content-Type: application/json" \
  -d '{"name": "Process CSV Data", "task_type": "batch_processing"}'

# Use Case 2: Email Notification
curl -X POST http://localhost:8080/tasks -H "Content-Type: application/json" \
  -d '{"name": "Alert All Admins", "task_type": "email_notification"}'

# Use Case 3: AI Job
curl -X POST http://localhost:8080/tasks -H "Content-Type: application/json" \
  -d '{"name": "Train Classifier Model", "task_type": "ai_job"}'
```

## REST API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Health check |
| GET | `/leader` | Current leader node |
| GET | `/workers` | Active workers list |
| GET | `/tasks` | All tasks (from DB) |
| POST | `/tasks` | Create a new task (`name`, `description`, `task_type`) |
| GET | `/assignments` | Task assignments per worker |
| GET | `/stats` | Task count by status and by type |
| GET | `/logs/:task_id` | Execution logs for a specific task |
| GET | `/events` | System events (leader election, failover, etc.) |

## Testing Failover

### Test 1: Verify leader election
```bash
docker-compose up --build
# Open http://localhost:3000 — observe the leader card
```

### Test 2: Create all 3 task types
Use the dashboard task form — select each task type and create tasks.

### Test 3: Kill the master and observe re-election
```bash
docker-compose stop worker-1
# Watch dashboard — new leader elected within seconds
# Event log shows: "Worker worker-2 elected as new LEADER"
```

### Test 4: Restart the worker
```bash
docker-compose start worker-1
# worker-1 rejoins as follower
```

### View Logs
```bash
docker-compose logs -f worker-1 worker-2 worker-3
docker-compose logs -f api-server
```

## Database Schema

```sql
CREATE TABLE tasks (
    id              SERIAL PRIMARY KEY,
    name            VARCHAR(255) NOT NULL,
    description     TEXT,
    task_type       VARCHAR(50) NOT NULL DEFAULT 'batch_processing',
    status          VARCHAR(50) DEFAULT 'pending',
    assigned_worker VARCHAR(255),
    retry_count     INTEGER NOT NULL DEFAULT 0,
    max_retries     INTEGER NOT NULL DEFAULT 3,
    created_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    completed_at    TIMESTAMP WITH TIME ZONE
);

CREATE TABLE task_logs (
    id         SERIAL PRIMARY KEY,
    task_id    INTEGER REFERENCES tasks(id),
    worker_id  VARCHAR(255),
    message    TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE system_events (
    id         SERIAL PRIMARY KEY,
    event_type VARCHAR(50) NOT NULL,
    source     VARCHAR(255),
    message    TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
```

## ZooKeeper ZNode Layout

```
/
├── workers/
│   ├── worker-1_0000000001  (ephemeral, sequential)
│   ├── worker-2_0000000002  (ephemeral, sequential)
│   └── worker-3_0000000003  (ephemeral, sequential)
├── tasks/
│   └── task_42             (created when UI submits task)
├── assignments/
│   └── worker-1_0000000001/
│       └── task_42         (created by MASTER, deleted after completion)
└── leader                  (contains ID of current master node)
```

## Key Distributed Systems Concepts Demonstrated

| Concept | Implementation |
|---------|---------------|
| Leader Election | Ephemeral sequential znodes, smallest sequence wins |
| Fault Tolerance | Chain-watch pattern triggers automatic re-election |
| Distributed Locks | Assignment znodes prevent duplicate execution |
| Watch Mechanism | ZK watchers for task detection and leader changes |
| CP Consistency | ZooKeeper guarantees linearizable reads/writes |
| Failover | Automatic, within seconds of master death |
| Coordination | ZK for sync; PostgreSQL for persistent task history |
| Task Retry | Auto-retry failed tasks up to max_retries |
| Load Balancing | Assign to worker with fewest current assignments |
| 3 Use Cases | Batch Processing, Email Notification, AI Job |

## Project Structure

```
DS_Casestudy/
├── api-server/
│   ├── main.go          # Gin server entry point
│   ├── routes.go        # REST API endpoints (+ /events)
│   ├── zk.go            # ZooKeeper client
│   ├── scheduler.go     # Task assignment + retry logic
│   ├── db.go            # PostgreSQL helpers + system_events
│   ├── go.mod
│   └── Dockerfile
├── worker/
│   ├── main.go          # Worker entry point
│   ├── election.go      # Leader election (ephemeral sequential znodes)
│   ├── tasks.go         # Type-specific task execution (batch/email/AI)
│   ├── zk.go            # ZK connection helpers
│   ├── go.mod
│   └── Dockerfile
├── frontend/
│   ├── src/
│   │   ├── App.tsx
│   │   ├── pages/Dashboard.tsx
│   │   ├── components/
│   │   │   ├── LeaderCard.tsx
│   │   │   ├── WorkerList.tsx
│   │   │   ├── TaskList.tsx     # with per-task logs panel
│   │   │   ├── TaskForm.tsx     # with task type selector
│   │   │   ├── StatsChart.tsx   # status + type breakdown
│   │   │   └── EventLog.tsx     # system events timeline
│   │   └── services/api.ts
│   ├── Dockerfile
│   └── nginx.conf
├── docker-compose.yml
├── CASE_STUDY.md          # Full case study document
└── README.md
```
#   D i s t r i b u t e d - J o b - S c h e d u l e r  
 