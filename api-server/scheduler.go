/*
This file defines the Scheduler component, which is responsible for polling ZooKeeper
to find pending tasks, assigning them to the least-loaded active worker, and scanning
the database to re-queue failed tasks that have retries remaining.
*/
package main

import (
	"database/sql"
	"fmt"
	"log"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-zookeeper/zk"
)

// Scheduler watches ZooKeeper tasks and assigns them to workers if this node is leader
type Scheduler struct {
	zk *ZKClient
	db *sql.DB
}

// NewScheduler creates a new Scheduler
func NewScheduler(zkClient *ZKClient, db *sql.DB) *Scheduler {
	return &Scheduler{zk: zkClient, db: db}
}

// Start begins the scheduler loop
func (s *Scheduler) Start() {
	log.Println("Scheduler started - watching for tasks and retries...")
	for {
		s.watchAndAssignTasks()
		s.retryFailedTasks()
		time.Sleep(2 * time.Second)
	}
}

// watchAndAssignTasks checks if we are leader and assigns pending tasks.
// It retrieves the list of pending tasks from ZooKeeper (/tasks), verifies
// their status in the database, and uses a round-robin / least-loaded hybrid
// allocation to match them to connected workers.
func (s *Scheduler) watchAndAssignTasks() {
	leader, err := s.zk.GetLeader()
	if err != nil || leader == "" {
		return
	}

	// Only the leader assigns tasks
	tasks, _, err := s.zk.conn.Children("/tasks")
	if err != nil {
		return
	}

	for _, taskNode := range tasks {
		taskPath := "/tasks/" + taskNode
		data, _, err := s.zk.conn.Get(taskPath)
		if err != nil {
			continue
		}

		taskIDStr := strings.TrimPrefix(taskNode, "task_")
		taskID, err := strconv.Atoi(taskIDStr)
		if err != nil {
			// Try using the data
			taskID, err = strconv.Atoi(string(data))
			if err != nil {
				continue
			}
		}

		// Check if already assigned in DB
		var status string
		err = s.db.QueryRow(`SELECT status FROM tasks WHERE id=$1`, taskID).Scan(&status)
		if err == sql.ErrNoRows || status != "pending" {
			continue
		}

		// Get available workers
		workers, err := s.zk.GetWorkers()
		if err != nil || len(workers) == 0 {
			continue
		}

		// Sort workers for deterministic assignment
		sort.Strings(workers)

		// Pick worker with fewest current assignments
		worker := s.pickWorker(workers)
		if worker == "" {
			continue
		}

		// Create assignment znode
		assignPath := fmt.Sprintf("/assignments/%s/%s", worker, taskNode)
		workerAssignDir := fmt.Sprintf("/assignments/%s", worker)

		// Ensure worker assignment dir exists
		exists, _, _ := s.zk.conn.Exists(workerAssignDir)
		if !exists {
			s.zk.conn.Create(workerAssignDir, []byte{}, 0, zk.WorldACL(zk.PermAll))
		}

		// Create task assignment (persistent node)
		_, err = s.zk.conn.Create(assignPath, []byte(fmt.Sprintf("%d", taskID)), 0, zk.WorldACL(zk.PermAll))
		if err != nil && err != zk.ErrNodeExists {
			log.Printf("Failed to create assignment %s: %v", assignPath, err)
			continue
		}

		// Update DB to mark as assigned
		_, err = s.db.Exec(`UPDATE tasks SET status='running', assigned_worker=$1 WHERE id=$2 AND status='pending'`, worker, taskID)
		if err != nil {
			log.Printf("Failed to update task %d: %v", taskID, err)
			continue
		}

		// Remove from /tasks now that it's assigned
		s.zk.conn.Delete(taskPath, -1)

		// Log assignment event
		CreateEvent(s.db, "task_assigned", "scheduler", fmt.Sprintf("Task %d assigned to %s", taskID, worker))

		log.Printf("Assigned task %d to worker %s", taskID, worker)
	}
}

// retryFailedTasks checks for failed tasks that can be retried.
// It queries the database for tasks marked as 'failed' that haven't exhausted
// their max_retries limit, increments their retry_count, resets their status
// to 'pending', and explicitly recreates their /tasks znode in ZooKeeper.
func (s *Scheduler) retryFailedTasks() {
	rows, err := s.db.Query(`SELECT id, name, retry_count, max_retries FROM tasks WHERE status='failed' AND retry_count < max_retries`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, retryCount, maxRetries int
		var name string
		if err := rows.Scan(&id, &name, &retryCount, &maxRetries); err != nil {
			continue
		}

		// Reset task to pending and increment retry count
		_, err := s.db.Exec(`UPDATE tasks SET status='pending', assigned_worker='', retry_count=retry_count+1 WHERE id=$1`, id)
		if err != nil {
			log.Printf("Failed to retry task %d: %v", id, err)
			continue
		}

		// Re-create the task znode
		if err := s.zk.CreateTaskZNode(id); err != nil {
			log.Printf("Failed to create retry znode for task %d: %v", id, err)
		}

		CreateEvent(s.db, "task_retried", "scheduler", fmt.Sprintf("Task '%s' (id=%d) retrying (attempt %d/%d)", name, id, retryCount+1, maxRetries))
		log.Printf("Retrying task %d '%s' (attempt %d/%d)", id, name, retryCount+1, maxRetries)
	}
}

// pickWorker picks the worker with the fewest active assignments
func (s *Scheduler) pickWorker(workers []string) string {
	minAssignments := -1
	bestWorker := ""

	for _, worker := range workers {
		workerPath := path.Join("/assignments", worker)
		exists, _, _ := s.zk.conn.Exists(workerPath)
		if !exists {
			return worker // No assignments yet
		}

		tasks, _, err := s.zk.conn.Children(workerPath)
		if err != nil {
			tasks = []string{}
		}

		count := len(tasks)
		if minAssignments == -1 || count < minAssignments {
			minAssignments = count
			bestWorker = worker
		}
	}

	return bestWorker
}
