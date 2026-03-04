package main

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes sets up all REST API routes
func RegisterRoutes(r *gin.Engine, zkClient *ZKClient, db *sql.DB) {
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// GET /leader - returns current leader node
	r.GET("/leader", func(c *gin.Context) {
		leader, err := zkClient.GetLeader()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"leader": "none", "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"leader": leader})
	})

	// GET /workers - returns list of active worker nodes from ZooKeeper
	r.GET("/workers", func(c *gin.Context) {
		workers, err := zkClient.GetWorkers()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if workers == nil {
			workers = []string{}
		}

		// Get leader to mark which worker is master
		leader, _ := zkClient.GetLeader()

		type WorkerInfo struct {
			ID       string `json:"id"`
			IsLeader bool   `json:"is_leader"`
			Status   string `json:"status"`
		}

		result := make([]WorkerInfo, 0, len(workers))
		for _, w := range workers {
			result = append(result, WorkerInfo{
				ID:       w,
				IsLeader: w == leader,
				Status:   "active",
			})
		}

		c.JSON(http.StatusOK, gin.H{"workers": result, "count": len(result)})
	})

	// GET /tasks - returns all tasks from PostgreSQL
	r.GET("/tasks", func(c *gin.Context) {
		tasks, err := GetAllTasks(db)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"tasks": tasks, "count": len(tasks)})
	})

	// POST /tasks - creates a new task
	r.POST("/tasks", func(c *gin.Context) {
		var req struct {
			Name        string `json:"name" binding:"required"`
			Description string `json:"description"`
			TaskType    string `json:"task_type"`
			Priority    string `json:"priority"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Validate task type
		validTypes := map[string]bool{
			"batch_processing":   true,
			"email_notification": true,
			"ai_job":             true,
		}
		if req.TaskType == "" {
			req.TaskType = "batch_processing"
		}
		if !validTypes[req.TaskType] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task_type. Must be: batch_processing, email_notification, or ai_job"})
			return
		}

		// Validate priority
		validPriorities := map[string]bool{
			"high":   true,
			"medium": true,
			"low":    true,
		}
		if req.Priority == "" {
			req.Priority = "medium"
		}
		if !validPriorities[req.Priority] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid priority. Must be: high, medium, or low"})
			return
		}

		task, err := CreateTask(db, req.Name, req.Description, req.TaskType, req.Priority)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Create task znode so workers can detect it
		if err := zkClient.CreateTaskZNode(task.ID); err != nil {
			// Non-fatal: log but still return success
			c.JSON(http.StatusCreated, gin.H{
				"task":    task,
				"warning": "task created in DB but ZK znode failed: " + err.Error(),
			})
			return
		}

		// Log event
		CreateEvent(db, "task_created", "api-server", "Task '"+task.Name+"' created (type: "+task.TaskType+", priority: "+task.Priority+")")

		c.JSON(http.StatusCreated, gin.H{"task": task})
	})

	// GET /assignments - returns assignments per worker from ZooKeeper + DB
	r.GET("/assignments", func(c *gin.Context) {
		assignments, err := GetAssignments(db)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Also get live ZK assignments
		zkAssignments, _ := zkClient.GetAssignmentZNodes()

		c.JSON(http.StatusOK, gin.H{
			"assignments":    assignments,
			"zk_assignments": zkAssignments,
		})
	})

	// GET /stats - returns task count by status and by type
	r.GET("/stats", func(c *gin.Context) {
		// Stats by status
		rows, err := db.Query(`SELECT status, COUNT(*) as count FROM tasks GROUP BY status`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		type Stat struct {
			Status string `json:"status"`
			Count  int    `json:"count"`
		}
		var stats []Stat
		for rows.Next() {
			var s Stat
			rows.Scan(&s.Status, &s.Count)
			stats = append(stats, s)
		}
		if stats == nil {
			stats = []Stat{}
		}

		// Stats by task type
		typeRows, err := db.Query(`SELECT task_type, COUNT(*) as count FROM tasks GROUP BY task_type`)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"stats": stats, "type_stats": []Stat{}})
			return
		}
		defer typeRows.Close()

		var typeStats []Stat
		for typeRows.Next() {
			var s Stat
			typeRows.Scan(&s.Status, &s.Count)
			typeStats = append(typeStats, s)
		}
		if typeStats == nil {
			typeStats = []Stat{}
		}

		c.JSON(http.StatusOK, gin.H{"stats": stats, "type_stats": typeStats})
	})

	// GET /logs/:task_id - returns logs for a specific task
	r.GET("/logs/:task_id", func(c *gin.Context) {
		taskIDStr := c.Param("task_id")
		taskID, err := strconv.Atoi(taskIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task_id"})
			return
		}

		rows, err := db.Query(`SELECT id, task_id, worker_id, message, created_at FROM task_logs WHERE task_id=$1 ORDER BY created_at ASC`, taskID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		type LogEntry struct {
			ID        int    `json:"id"`
			TaskID    int    `json:"task_id"`
			WorkerID  string `json:"worker_id"`
			Message   string `json:"message"`
			CreatedAt string `json:"created_at"`
		}
		var logs []LogEntry
		for rows.Next() {
			var l LogEntry
			rows.Scan(&l.ID, &l.TaskID, &l.WorkerID, &l.Message, &l.CreatedAt)
			logs = append(logs, l)
		}
		if logs == nil {
			logs = []LogEntry{}
		}
		c.JSON(http.StatusOK, gin.H{"logs": logs})
	})

	// GET /events - returns recent system events
	r.GET("/events", func(c *gin.Context) {
		limitStr := c.DefaultQuery("limit", "50")
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			limit = 50
		}

		events, err := GetRecentEvents(db, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"events": events, "count": len(events)})
	})
}
