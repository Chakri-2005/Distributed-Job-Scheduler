/*
This file defines the TaskExecutor, which represents the worker logic for a node.
It continuously watches its specific ZooKeeper assignments directory for new tasks,
executes them by simulating work (e.g., AI modeling, batch processing),
and logs the execution trace directly into the shared PostgreSQL database.
*/
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/go-zookeeper/zk"
)

// TaskExecutor watches assignments and executes tasks for this node
type TaskExecutor struct {
	nodeID   string
	zkClient *ZKClient
	db       *sql.DB
}

// NewTaskExecutor creates a new TaskExecutor
func NewTaskExecutor(nodeID string, zkClient *ZKClient, db *sql.DB) *TaskExecutor {
	return &TaskExecutor{
		nodeID:   nodeID,
		zkClient: zkClient,
		db:       db,
	}
}

// Watch continuously watches for task assignments and executes them.
// It resolves the node's specific ephemeral znode name and sets up a ZooKeeper watch.
func (te *TaskExecutor) Watch() {
	for {
		myNodeName := te.getMyNodeName()
		if myNodeName != "" {
			te.watchAssignments(myNodeName)
		}
		time.Sleep(2 * time.Second)
	}
}

// getMyNodeName finds our ephemeral sequential node name in /workers
func (te *TaskExecutor) getMyNodeName() string {
	children, _, err := te.zkClient.conn.Children("/workers")
	if err != nil {
		return ""
	}
	for _, child := range children {
		if strings.HasPrefix(child, te.nodeID+"_") {
			return child
		}
	}
	return ""
}

// watchAssignments watches /assignments/<nodeID>/ for new tasks.
// Upon detecting a new child node (a new task assignment), it spawns a goroutine
// to execute the task concurrently.
func (te *TaskExecutor) watchAssignments(nodeID string) {
	assignPath := fmt.Sprintf("/assignments/%s", nodeID)

	exists, _, _ := te.zkClient.conn.Exists(assignPath)
	if !exists {
		te.zkClient.conn.Create(assignPath, []byte{}, 0, zk.WorldACL(zk.PermAll))
	}

	tasks, _, ch, err := te.zkClient.conn.ChildrenW(assignPath)
	if err != nil {
		return
	}

	for _, taskNode := range tasks {
		go te.executeTask(nodeID, taskNode)
	}

	select {
	case event := <-ch:
		if event.Type == zk.EventNodeChildrenChanged {
			log.Printf("Node %s: new assignment detected!", te.nodeID)
		}
	case <-time.After(5 * time.Second):
	}
}

// executeTask simulates task execution based on task type.
// It retrieves the task payload, updates the DB status to 'running',
// delegates to a specific mock runner (AI, Batch, or Email), and handles
// success/failure database updates and WebSocket broadcasting upon completion.
func (te *TaskExecutor) executeTask(nodeID, taskNode string) {
	taskPath := fmt.Sprintf("/assignments/%s/%s", nodeID, taskNode)

	data, _, err := te.zkClient.conn.Get(taskPath)
	if err != nil {
		return
	}

	taskIDStr := string(data)
	taskID, err := strconv.Atoi(taskIDStr)
	if err != nil {
		log.Printf("Node %s: invalid task ID %s", te.nodeID, taskIDStr)
		return
	}

	var taskType, taskName string
	err = te.db.QueryRow(`SELECT COALESCE(task_type, 'batch_processing'), name FROM tasks WHERE id=$1`, taskID).Scan(&taskType, &taskName)
	if err != nil {
		taskType = "batch_processing"
		taskName = "Unknown"
	}

	log.Printf("Node %s: 🚀 Starting %s task %d (%s)", te.nodeID, taskType, taskID, taskName)
	te.writeLog(taskID, fmt.Sprintf("Worker %s started executing %s task '%s' (id=%d)", te.nodeID, taskType, taskName, taskID))

	// Update status to running
	te.db.Exec(`UPDATE tasks SET status='running', assigned_worker=$1 WHERE id=$2`, te.nodeID, taskID)

	// Broadcast running status
	te.broadcastTaskUpdate(taskID, "running", te.nodeID)

	var success bool
	switch taskType {
	case "email_notification":
		success = te.executeEmailTask(taskID, taskName)
	case "ai_job":
		success = te.executeAITask(taskID, taskName)
	default:
		success = te.executeBatchTask(taskID, taskName)
	}

	if success {
		_, err = te.db.Exec(`UPDATE tasks SET status='completed', completed_at=NOW() WHERE id=$1`, taskID)
		if err != nil {
			log.Printf("Node %s: failed to update task %d: %v", te.nodeID, taskID, err)
			return
		}
		te.writeLog(taskID, fmt.Sprintf("Worker %s completed task '%s' (id=%d) successfully ✅", te.nodeID, taskName, taskID))
		te.writeEvent("task_completed", fmt.Sprintf("Task '%s' (id=%d) completed by %s", taskName, taskID, te.nodeID))
		te.broadcastTaskUpdate(taskID, "completed", te.nodeID)
		log.Printf("Node %s: ✅ Task %d COMPLETED!", te.nodeID, taskID)
	} else {
		te.db.Exec(`UPDATE tasks SET status='failed' WHERE id=$1`, taskID)
		te.writeLog(taskID, fmt.Sprintf("Worker %s: task '%s' (id=%d) FAILED ❌ - will be retried", te.nodeID, taskName, taskID))
		te.writeEvent("task_failed", fmt.Sprintf("Task '%s' (id=%d) failed on %s", taskName, taskID, te.nodeID))
		te.broadcastTaskUpdate(taskID, "failed", te.nodeID)
		log.Printf("Node %s: ❌ Task %d FAILED!", te.nodeID, taskID)
	}

	te.zkClient.conn.Delete(taskPath, -1)
}

func (te *TaskExecutor) executeBatchTask(taskID int, taskName string) bool {
	totalRecords := 50 + rand.Intn(150)
	steps := 3 + rand.Intn(3)
	recordsPerStep := totalRecords / steps

	te.writeLog(taskID, fmt.Sprintf("📦 Batch processing: %d records to process", totalRecords))

	for i := 1; i <= steps; i++ {
		processed := i * recordsPerStep
		if i == steps {
			processed = totalRecords
		}
		sleepMs := 1500 + rand.Intn(1500)
		time.Sleep(time.Duration(sleepMs) * time.Millisecond)
		te.writeLog(taskID, fmt.Sprintf("📦 Batch: processed %d/%d records (%.0f%%)", processed, totalRecords, float64(processed)/float64(totalRecords)*100))
	}

	if rand.Intn(10) == 0 {
		te.writeLog(taskID, "📦 Batch processing encountered an error: I/O timeout")
		return false
	}
	return true
}

func (te *TaskExecutor) executeEmailTask(taskID int, taskName string) bool {
	recipients := 3 + rand.Intn(8)
	te.writeLog(taskID, fmt.Sprintf("📧 Email notification: sending to %d recipients", recipients))

	for i := 1; i <= recipients; i++ {
		sleepMs := 200 + rand.Intn(300)
		time.Sleep(time.Duration(sleepMs) * time.Millisecond)
		te.writeLog(taskID, fmt.Sprintf("📧 Email sent to recipient %d/%d", i, recipients))
	}

	if rand.Intn(10) == 0 {
		te.writeLog(taskID, "📧 Email delivery failed: SMTP connection refused")
		return false
	}

	te.writeLog(taskID, fmt.Sprintf("📧 All %d emails delivered successfully", recipients))
	return true
}

func (te *TaskExecutor) executeAITask(taskID int, taskName string) bool {
	epochs := 3 + rand.Intn(3)
	te.writeLog(taskID, fmt.Sprintf("🤖 AI Job: starting model training (%d epochs)", epochs))

	for i := 1; i <= epochs; i++ {
		sleepMs := 1500 + rand.Intn(2000)
		time.Sleep(time.Duration(sleepMs) * time.Millisecond)
		loss := 1.0 - (float64(i) / float64(epochs) * 0.8) + (rand.Float64() * 0.1)
		accuracy := float64(i)/float64(epochs)*85 + rand.Float64()*10
		te.writeLog(taskID, fmt.Sprintf("🤖 Epoch %d/%d — loss: %.4f, accuracy: %.1f%%", i, epochs, loss, accuracy))
	}

	te.writeLog(taskID, "🤖 Running inference on test dataset...")
	time.Sleep(time.Duration(1000+rand.Intn(2000)) * time.Millisecond)
	te.writeLog(taskID, fmt.Sprintf("🤖 Inference complete — accuracy: %.1f%%", 85+rand.Float64()*12))

	if rand.Intn(10) == 0 {
		te.writeLog(taskID, "🤖 AI Job failed: GPU memory exhausted")
		return false
	}
	return true
}

func (te *TaskExecutor) writeLog(taskID int, message string) {
	te.db.Exec(
		`INSERT INTO task_logs (task_id, worker_id, message) VALUES ($1, $2, $3)`,
		taskID, te.nodeID, message,
	)
}

func (te *TaskExecutor) writeEvent(eventType, message string) {
	te.db.Exec(
		`INSERT INTO system_events (event_type, source, message) VALUES ($1, $2, $3)`,
		eventType, te.nodeID, message,
	)
}

func (te *TaskExecutor) broadcastTaskUpdate(taskID int, status, worker string) {
	if hub == nil {
		return
	}
	msg, _ := json.Marshal(map[string]interface{}{
		"type":    "task_updated",
		"task_id": taskID,
		"status":  status,
		"worker":  worker,
	})
	hub.Broadcast(msg)
}
