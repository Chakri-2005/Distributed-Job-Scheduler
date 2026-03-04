package main

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/lib/pq"
)

// Task represents a job in the system
type Task struct {
	ID             int        `json:"id"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	TaskType       string     `json:"task_type"`
	Priority       string     `json:"priority"` // high, medium, low
	Status         string     `json:"status"`   // pending, running, completed, failed
	AssignedWorker string     `json:"assigned_worker,omitempty"`
	RetryCount     int        `json:"retry_count"`
	MaxRetries     int        `json:"max_retries"`
	CreatedAt      time.Time  `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

// SystemEvent represents a system-level event (leader changes, worker joins, etc.)
type SystemEvent struct {
	ID        int       `json:"id"`
	EventType string    `json:"event_type"` // leader_elected, worker_joined, worker_left, task_assigned, task_retried, failover
	Source    string    `json:"source"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// InitDB creates and returns a database connection
func InitDB(dsn string) (*sql.DB, error) {
	var db *sql.DB
	var err error

	// Retry up to 10 times with 3-second interval (for Docker readiness)
	for i := 0; i < 10; i++ {
		db, err = sql.Open("postgres", dsn)
		if err == nil {
			if pingErr := db.Ping(); pingErr == nil {
				log.Println("Connected to PostgreSQL")
				return db, nil
			}
		}
		log.Printf("DB not ready yet (attempt %d/10), retrying in 3s...", i+1)
		time.Sleep(3 * time.Second)
	}
	return nil, err
}

// RunMigrations ensures the tables exist
func RunMigrations(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS tasks (
		id              SERIAL PRIMARY KEY,
		name            VARCHAR(255) NOT NULL,
		description     TEXT,
		task_type       VARCHAR(50) NOT NULL DEFAULT 'batch_processing',
		priority        VARCHAR(20) NOT NULL DEFAULT 'medium',
		status          VARCHAR(50) NOT NULL DEFAULT 'pending',
		assigned_worker VARCHAR(255),
		retry_count     INTEGER NOT NULL DEFAULT 0,
		max_retries     INTEGER NOT NULL DEFAULT 3,
		created_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
		completed_at    TIMESTAMP WITH TIME ZONE
	);

	CREATE TABLE IF NOT EXISTS task_logs (
		id         SERIAL PRIMARY KEY,
		task_id    INTEGER REFERENCES tasks(id),
		worker_id  VARCHAR(255),
		message    TEXT,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS system_events (
		id         SERIAL PRIMARY KEY,
		event_type VARCHAR(50) NOT NULL,
		source     VARCHAR(255),
		message    TEXT,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);
	`

	// Run base schema
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// Add columns if they don't exist (for existing databases)
	alterStatements := []string{
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS task_type VARCHAR(50) NOT NULL DEFAULT 'batch_processing'`,
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS priority VARCHAR(20) NOT NULL DEFAULT 'medium'`,
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS retry_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS max_retries INTEGER NOT NULL DEFAULT 3`,
	}
	for _, stmt := range alterStatements {
		db.Exec(stmt) // Ignore errors (column may already exist)
	}

	log.Println("Database migrations applied successfully")
	return nil
}

// CreateTask inserts a new task into the DB and returns it
func CreateTask(db *sql.DB, name, description, taskType, priority string) (*Task, error) {
	if taskType == "" {
		taskType = "batch_processing"
	}
	if priority == "" {
		priority = "medium"
	}
	var task Task
	err := db.QueryRow(
		`INSERT INTO tasks (name, description, task_type, priority, status, created_at) VALUES ($1, $2, $3, $4, 'pending', NOW()) RETURNING id, name, description, task_type, priority, status, retry_count, max_retries, created_at`,
		name, description, taskType, priority,
	).Scan(&task.ID, &task.Name, &task.Description, &task.TaskType, &task.Priority, &task.Status, &task.RetryCount, &task.MaxRetries, &task.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// GetAllTasks returns all tasks ordered by priority then created_at desc
func GetAllTasks(db *sql.DB) ([]Task, error) {
	rows, err := db.Query(`SELECT id, name, description, task_type, COALESCE(priority,'medium'), status, COALESCE(assigned_worker,''), retry_count, max_retries, created_at, completed_at FROM tasks ORDER BY CASE priority WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 2 END, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var assignedWorker string
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.TaskType, &t.Priority, &t.Status, &assignedWorker, &t.RetryCount, &t.MaxRetries, &t.CreatedAt, &t.CompletedAt); err != nil {
			return nil, err
		}
		if assignedWorker != "" {
			t.AssignedWorker = assignedWorker
		}
		tasks = append(tasks, t)
	}
	if tasks == nil {
		tasks = []Task{}
	}
	return tasks, nil
}

// GetAssignments returns tasks grouped by worker
func GetAssignments(db *sql.DB) (map[string][]Task, error) {
	tasks, err := GetAllTasks(db)
	if err != nil {
		return nil, err
	}

	assignments := make(map[string][]Task)
	for _, t := range tasks {
		if t.AssignedWorker != "" {
			assignments[t.AssignedWorker] = append(assignments[t.AssignedWorker], t)
		}
	}
	return assignments, nil
}

// CreateEvent logs a system event
func CreateEvent(db *sql.DB, eventType, source, message string) {
	_, err := db.Exec(
		`INSERT INTO system_events (event_type, source, message) VALUES ($1, $2, $3)`,
		eventType, source, message,
	)
	if err != nil {
		log.Printf("Failed to log event: %v", err)
	}
}

// GetRecentEvents returns the most recent system events
func GetRecentEvents(db *sql.DB, limit int) ([]SystemEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(`SELECT id, event_type, COALESCE(source,''), message, created_at FROM system_events ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []SystemEvent
	for rows.Next() {
		var e SystemEvent
		if err := rows.Scan(&e.ID, &e.EventType, &e.Source, &e.Message, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	if events == nil {
		events = []SystemEvent{}
	}
	return events, nil
}
