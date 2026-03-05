/*
Package main implements the standalone Worker Node for the Distributed Job Scheduler.
This executable connects to ZooKeeper to participate in leader election, and
polls its assignment queue to simulate processing of distributed tasks.
*/
package main

import (
	"fmt"
	"log"
	"os"
	"time"
)

// main is the entry point for the standalone worker process.
// It initializes database and ZooKeeper connections, registers the node for election,
// and spawns the background task watcher.
func main() {
	workerID := os.Getenv("WORKER_ID")
	if workerID == "" {
		hostname, _ := os.Hostname()
		workerID = hostname
	}

	zkHost := getEnv("ZK_HOST", "localhost:2181")
	dbDSN := getEnv("DATABASE_URL", "host=localhost user=postgres password=postgres dbname=jobscheduler sslmode=disable")

	fmt.Printf("=== Worker %s starting ===\n", workerID)

	// Connect to ZooKeeper
	zkClient, err := NewZKClient(zkHost)
	if err != nil {
		log.Fatalf("Worker %s: failed to connect to ZooKeeper: %v", workerID, err)
	}
	defer zkClient.Close()

	// Connect to DB
	db, err := InitWorkerDB(dbDSN)
	if err != nil {
		log.Fatalf("Worker %s: failed to connect to DB: %v", workerID, err)
	}
	defer db.Close()

	// Start election process
	elector := NewElector(workerID, zkClient, db)
	if err := elector.Register(); err != nil {
		log.Fatalf("Worker %s: failed to register: %v", workerID, err)
	}

	// Start watching for assigned tasks
	taskWatcher := NewTaskWatcher(workerID, zkClient, db)
	go taskWatcher.Watch()

	// Keep alive
	log.Printf("Worker %s is running. Press Ctrl+C to stop.", workerID)
	for {
		time.Sleep(30 * time.Second)
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
