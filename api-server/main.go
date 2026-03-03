package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	zkHost := getEnv("ZK_HOST", "localhost:2181")
	dbDSN := getEnv("DATABASE_URL", "host=localhost user=postgres password=postgres dbname=jobscheduler sslmode=disable")

	// Initialize DB
	db, err := InitDB(dbDSN)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer db.Close()

	// Run migrations
	if err := RunMigrations(db); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Initialize ZooKeeper
	zkClient, err := NewZKClient(zkHost)
	if err != nil {
		log.Fatalf("Failed to connect to ZooKeeper: %v", err)
	}
	defer zkClient.Close()

	// Ensure base znodes exist
	zkClient.EnsureZNodes()

	// Start scheduler (watches ZK tasks and assigns to workers if leader)
	scheduler := NewScheduler(zkClient, db)
	go scheduler.Start()

	// Setup Gin
	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	RegisterRoutes(r, zkClient, db)

	port := getEnv("PORT", "8080")
	fmt.Printf("API Server starting on port %s\n", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
