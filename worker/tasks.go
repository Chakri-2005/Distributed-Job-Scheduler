/*
This file defines the TaskWatcher struct which monitors ZooKeeper for task assignments.
When a task is assigned to this worker's specific node queue, it executes the
simulated logic (Batch, Email, AI) and reports success/failure back to PostgreSQL.
*/
package main

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/go-zookeeper/zk"
)

// TaskWatcher watches /assignments/<workerID>/ for new tasks to execute
type TaskWatcher struct {
	workerID string
	zkClient *ZKClient
	db       *sql.DB
}

// NewTaskWatcher creates a new TaskWatcher
func NewTaskWatcher(workerID string, zkClient *ZKClient, db *sql.DB) *TaskWatcher {
	return &TaskWatcher{
		workerID: workerID,
		zkClient: zkClient,
		db:       db,
	}
}

// Watch continuously watches for task assignments and executes them
func (tw *TaskWatcher) Watch() {
	// Derive our node name from /workers listing
	for {
		myNodeName := tw.getMyNodeName()
		if myNodeName != "" {
			tw.watchAssignments(myNodeName)
		}
		time.Sleep(2 * time.Second)
	}
}

// getMyNodeName finds our ephemeral sequential node name in /workers
func (tw *TaskWatcher) getMyNodeName() string {
	children, _, err := tw.zkClient.conn.Children("/workers")
	if err != nil {
		return ""
	}
	for _, child := range children {
		// Node name is workerID_ prefix
		if strings.HasPrefix(child, tw.workerID+"_") {
			return child
		}
	}
	return ""
}

// watchAssignments watches /assignments/<nodeID>/ for new tasks
func (tw *TaskWatcher) watchAssignments(nodeID string) {
	assignPath := fmt.Sprintf("/assignments/%s", nodeID)

	// Ensure our assignment dir exists
	exists, _, _ := tw.zkClient.conn.Exists(assignPath)
	if !exists {
		tw.zkClient.conn.Create(assignPath, []byte{}, 0, zk.WorldACL(zk.PermAll))
	}

	tasks, _, ch, err := tw.zkClient.conn.ChildrenW(assignPath)
	if err != nil {
		return
	}

	// Execute any already-waiting tasks
	for _, taskNode := range tasks {
		go tw.executeTask(nodeID, taskNode)
	}

	// Block and wait for new assignments
	select {
	case event := <-ch:
		if event.Type == zk.EventNodeChildrenChanged {
			log.Printf("Worker %s: new assignment detected!", tw.workerID)
		}
	case <-time.After(5 * time.Second):
	}
}

// executeTask simulates task execution based on task type.
// It retrieves the task payload, switches the DB status to 'running', sleeps
// based on parameters to simulate work elapsed time, and deterministically
// processes 10% simulated failure rates.
func (tw *TaskWatcher) executeTask(nodeID, taskNode string) {
	taskPath := fmt.Sprintf("/assignments/%s/%s", nodeID, taskNode)

	// Read task ID from the znode
	data, _, err := tw.zkClient.conn.Get(taskPath)
	if err != nil {
		return // Already executed or removed
	}

	taskIDStr := string(data)
	taskID, err := strconv.Atoi(taskIDStr)
	if err != nil {
		log.Printf("Worker %s: invalid task ID %s", tw.workerID, taskIDStr)
		return
	}

	// Read task type from DB
	var taskType, taskName string
	err = tw.db.QueryRow(`SELECT COALESCE(task_type, 'batch_processing'), name FROM tasks WHERE id=$1`, taskID).Scan(&taskType, &taskName)
	if err != nil {
		taskType = "batch_processing"
		taskName = "Unknown"
	}

	log.Printf("Worker %s: 🚀 Starting execution of %s task %d (%s)", tw.workerID, taskType, taskID, taskName)
	tw.writeLog(taskID, fmt.Sprintf("Worker %s started executing %s task '%s' (id=%d)", tw.workerID, taskType, taskName, taskID))

	// Update status to running
	tw.db.Exec(`UPDATE tasks SET status='running', assigned_worker=$1 WHERE id=$2`, tw.workerID, taskID)

	// Execute based on task type
	var success bool
	switch taskType {
	case "email_notification":
		success = tw.executeEmailTask(taskID, taskName)
	case "ai_job":
		success = tw.executeAITask(taskID, taskName)
	default:
		success = tw.executeBatchTask(taskID, taskName)
	}

	if success {
		// Update task status in DB to completed
		_, err = tw.db.Exec(
			`UPDATE tasks SET status='completed', completed_at=NOW() WHERE id=$1`,
			taskID,
		)
		if err != nil {
			log.Printf("Worker %s: failed to update task %d: %v", tw.workerID, taskID, err)
			tw.writeLog(taskID, fmt.Sprintf("ERROR completing task %d: %v", taskID, err))
			return
		}
		tw.writeLog(taskID, fmt.Sprintf("Worker %s completed task '%s' (id=%d) successfully ✅", tw.workerID, taskName, taskID))
		tw.writeEvent("task_completed", fmt.Sprintf("Task '%s' (id=%d) completed by %s", taskName, taskID, tw.workerID))
		log.Printf("Worker %s: ✅ Task %d COMPLETED!", tw.workerID, taskID)
	} else {
		// Mark as failed - scheduler will retry if retry_count < max_retries
		tw.db.Exec(`UPDATE tasks SET status='failed' WHERE id=$1`, taskID)
		tw.writeLog(taskID, fmt.Sprintf("Worker %s: task '%s' (id=%d) FAILED ❌ - will be retried", tw.workerID, taskName, taskID))
		tw.writeEvent("task_failed", fmt.Sprintf("Task '%s' (id=%d) failed on %s", taskName, taskID, tw.workerID))
		log.Printf("Worker %s: ❌ Task %d FAILED!", tw.workerID, taskID)
	}

	// Remove assignment znode to prevent duplicate execution
	tw.zkClient.conn.Delete(taskPath, -1)
}

// executeBatchTask simulates batch file processing (5-8 seconds)
func (tw *TaskWatcher) executeBatchTask(taskID int, taskName string) bool {
	totalRecords := 50 + rand.Intn(150) // 50-200 records
	steps := 3 + rand.Intn(3)           // 3-5 steps
	recordsPerStep := totalRecords / steps

	tw.writeLog(taskID, fmt.Sprintf("📦 Batch processing: %d records to process", totalRecords))

	for i := 1; i <= steps; i++ {
		processed := i * recordsPerStep
		if i == steps {
			processed = totalRecords
		}
		sleepMs := 1500 + rand.Intn(1500) // 1.5-3s per step
		time.Sleep(time.Duration(sleepMs) * time.Millisecond)
		tw.writeLog(taskID, fmt.Sprintf("📦 Batch: processed %d/%d records (%.0f%%)", processed, totalRecords, float64(processed)/float64(totalRecords)*100))
		log.Printf("Worker %s: task %d batch processing %d/%d", tw.workerID, taskID, processed, totalRecords)
	}

	// ~10% chance of failure to demo retry
	if rand.Intn(10) == 0 {
		tw.writeLog(taskID, "📦 Batch processing encountered an error: I/O timeout")
		return false
	}
	return true
}

// executeEmailTask simulates email notification sending (2-3 seconds)
func (tw *TaskWatcher) executeEmailTask(taskID int, taskName string) bool {
	recipients := 3 + rand.Intn(8) // 3-10 recipients
	tw.writeLog(taskID, fmt.Sprintf("📧 Email notification: sending to %d recipients", recipients))

	for i := 1; i <= recipients; i++ {
		sleepMs := 200 + rand.Intn(300) // 200-500ms per email
		time.Sleep(time.Duration(sleepMs) * time.Millisecond)
		tw.writeLog(taskID, fmt.Sprintf("📧 Email sent to recipient %d/%d", i, recipients))
	}

	// ~10% chance of failure
	if rand.Intn(10) == 0 {
		tw.writeLog(taskID, "📧 Email delivery failed: SMTP connection refused")
		return false
	}

	tw.writeLog(taskID, fmt.Sprintf("📧 All %d emails delivered successfully", recipients))
	return true
}

// executeAITask simulates AI job processing (8-12 seconds)
func (tw *TaskWatcher) executeAITask(taskID int, taskName string) bool {
	epochs := 3 + rand.Intn(3) // 3-5 epochs
	tw.writeLog(taskID, fmt.Sprintf("🤖 AI Job: starting model training (%d epochs)", epochs))

	// Training phase
	for i := 1; i <= epochs; i++ {
		sleepMs := 1500 + rand.Intn(2000) // 1.5-3.5s per epoch
		time.Sleep(time.Duration(sleepMs) * time.Millisecond)
		loss := 1.0 - (float64(i) / float64(epochs) * 0.8) + (rand.Float64() * 0.1)
		accuracy := float64(i)/float64(epochs)*85 + rand.Float64()*10
		tw.writeLog(taskID, fmt.Sprintf("🤖 Epoch %d/%d — loss: %.4f, accuracy: %.1f%%", i, epochs, loss, accuracy))
		log.Printf("Worker %s: task %d AI training epoch %d/%d", tw.workerID, taskID, i, epochs)
	}

	// Inference phase
	tw.writeLog(taskID, "🤖 Running inference on test dataset...")
	time.Sleep(time.Duration(1000+rand.Intn(2000)) * time.Millisecond)
	tw.writeLog(taskID, fmt.Sprintf("🤖 Inference complete — accuracy: %.1f%%", 85+rand.Float64()*12))

	// ~10% chance of failure
	if rand.Intn(10) == 0 {
		tw.writeLog(taskID, "🤖 AI Job failed: GPU memory exhausted")
		return false
	}

	return true
}

// writeLog writes a log entry to the task_logs table
func (tw *TaskWatcher) writeLog(taskID int, message string) {
	tw.db.Exec(
		`INSERT INTO task_logs (task_id, worker_id, message) VALUES ($1, $2, $3)`,
		taskID, tw.workerID, message,
	)
}

// writeEvent writes a system event to the system_events table
func (tw *TaskWatcher) writeEvent(eventType, message string) {
	tw.db.Exec(
		`INSERT INTO system_events (event_type, source, message) VALUES ($1, $2, $3)`,
		eventType, tw.workerID, message,
	)
}
