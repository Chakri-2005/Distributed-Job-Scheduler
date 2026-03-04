package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes sets up all REST API routes
func RegisterRoutes(r *gin.Engine, zkClient *ZKClient, db *sql.DB) {
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// GET /node-info - returns this node's role and metadata
	r.GET("/node-info", func(c *gin.Context) {
		if clusterNode == nil {
			c.JSON(http.StatusOK, gin.H{
				"node_id": "unknown",
				"role":    "slave",
				"ip":      "",
				"port":    "",
				"status":  "starting",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"node_id":   clusterNode.NodeID,
			"role":      clusterNode.Role,
			"ip":        clusterNode.IP,
			"port":      clusterNode.Port,
			"status":    "active",
			"is_leader": clusterNode.IsLeader,
		})
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

		leader, _ := zkClient.GetLeader()

		type WorkerInfo struct {
			ID       string `json:"id"`
			IsLeader bool   `json:"is_leader"`
			Status   string `json:"status"`
		}

		result := make([]WorkerInfo, 0, len(workers))
		for _, w := range workers {
			status := "active"
			// Check node status from /nodes
			workerBase := w
			// Try to get the worker base name (before the _sequence)
			if idx := len(w) - 1; idx > 0 {
				for i := idx; i >= 0; i-- {
					if w[i] == '_' {
						workerBase = w[:i]
						break
					}
				}
			}
			nodePath := "/nodes/" + workerBase
			data, _, err := zkClient.conn.Get(nodePath)
			if err == nil {
				var info NodeInfo
				if json.Unmarshal(data, &info) == nil {
					status = info.Status
				}
			}

			result = append(result, WorkerInfo{
				ID:       w,
				IsLeader: w == leader,
				Status:   status,
			})
		}

		c.JSON(http.StatusOK, gin.H{"workers": result, "count": len(result)})
	})

	// GET /nodes - returns all cluster nodes (from /nodes in ZK)
	r.GET("/nodes", func(c *gin.Context) {
		nodes, err := GetClusterNodes(zkClient)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if nodes == nil {
			nodes = []NodeInfo{}
		}
		c.JSON(http.StatusOK, gin.H{"nodes": nodes, "count": len(nodes)})
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

		validTypes := map[string]bool{
			"batch_processing":   true,
			"email_notification": true,
			"ai_job":             true,
		}
		if req.TaskType == "" {
			req.TaskType = "batch_processing"
		}
		if !validTypes[req.TaskType] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task_type"})
			return
		}

		validPriorities := map[string]bool{"high": true, "medium": true, "low": true}
		if req.Priority == "" {
			req.Priority = "medium"
		}
		if !validPriorities[req.Priority] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid priority"})
			return
		}

		task, err := CreateTask(db, req.Name, req.Description, req.TaskType, req.Priority)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if err := zkClient.CreateTaskZNode(task.ID); err != nil {
			c.JSON(http.StatusCreated, gin.H{
				"task":    task,
				"warning": "task created in DB but ZK znode failed: " + err.Error(),
			})
			return
		}

		// Determine source node
		sourceNode := "api-server"
		if clusterNode != nil {
			sourceNode = clusterNode.NodeID
		}

		CreateEvent(db, "task_created", sourceNode,
			"Task '"+task.Name+"' created (type: "+task.TaskType+", priority: "+task.Priority+")")

		// Broadcast task creation via WebSocket
		if hub != nil {
			msg, _ := json.Marshal(map[string]interface{}{
				"type":    "task_created",
				"task_id": task.ID,
				"name":    task.Name,
				"status":  "pending",
			})
			hub.Broadcast(msg)
		}

		c.JSON(http.StatusCreated, gin.H{"task": task})
	})

	// PUT /workers/:id/deactivate - deactivate a worker (master only)
	r.PUT("/workers/:id/deactivate", func(c *gin.Context) {
		if clusterNode != nil && !clusterNode.IsLeader {
			c.JSON(http.StatusForbidden, gin.H{"error": "Only the master node can deactivate workers"})
			return
		}

		workerID := c.Param("id")
		if err := DeactivateWorker(zkClient, db, workerID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Worker " + workerID + " deactivated"})
	})

	// PUT /workers/:id/activate - activate a worker (master only)
	r.PUT("/workers/:id/activate", func(c *gin.Context) {
		if clusterNode != nil && !clusterNode.IsLeader {
			c.JSON(http.StatusForbidden, gin.H{"error": "Only the master node can activate workers"})
			return
		}

		workerID := c.Param("id")
		if err := ActivateWorker(zkClient, db, workerID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Worker " + workerID + " activated"})
	})

	// GET /assignments - returns assignments per worker from DB
	r.GET("/assignments", func(c *gin.Context) {
		assignments, err := GetAssignments(db)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		zkAssignments, _ := zkClient.GetAssignmentZNodes()

		c.JSON(http.StatusOK, gin.H{
			"assignments":    assignments,
			"zk_assignments": zkAssignments,
		})
	})

	// GET /stats - returns task count by status and by type
	r.GET("/stats", func(c *gin.Context) {
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

	// WebSocket endpoint
	r.GET("/ws", HandleWebSocket)
}
