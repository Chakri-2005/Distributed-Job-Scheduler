package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	nodeID := getEnv("NODE_ID", "master")
	port := getEnv("PORT", "8080")
	zkHost := getEnv("ZK_HOST", "localhost:2181")
	dbDSN := getEnv("DATABASE_URL", "host=localhost user=postgres password=postgres dbname=jobscheduler sslmode=disable")
	localIP := getEnv("NODE_IP", GetLocalIP())

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

	// Initialize WebSocket hub
	hub = NewWSHub()
	go hub.Run()

	// Initialize cluster node (handles election + task assignment)
	clusterNode = NewClusterNode(nodeID, port, localIP, zkClient, db)
	if err := clusterNode.Register(); err != nil {
		log.Fatalf("Failed to register node: %v", err)
	}

	// Start task executor (watches for assigned tasks and executes them)
	executor := NewTaskExecutor(nodeID, zkClient, db)
	go executor.Watch()

	// Start retry watcher
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

	// Register API routes
	RegisterRoutes(r, zkClient, db)

	// Serve static frontend files
	distPath := getEnv("FRONTEND_DIST", "./frontend-dist")
	if info, err := os.Stat(distPath); err == nil && info.IsDir() {
		// Serve static files from dist directory
		r.Static("/assets", filepath.Join(distPath, "assets"))
		r.StaticFile("/vite.svg", filepath.Join(distPath, "vite.svg"))

		// SPA fallback: serve index.html for all non-API, non-asset routes
		r.NoRoute(func(c *gin.Context) {
			c.File(filepath.Join(distPath, "index.html"))
		})

		log.Printf("Serving frontend from: %s", distPath)
	} else {
		log.Printf("No frontend dist found at %s, API-only mode", distPath)
		r.NoRoute(func(c *gin.Context) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Not found. Frontend not available in this mode.",
				"node":  nodeID,
				"port":  port,
			})
		})
	}

	fmt.Printf("\n🚀 Node %s starting on 0.0.0.0:%s (IP: %s)\n", nodeID, port, localIP)
	if err := r.Run("0.0.0.0:" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
