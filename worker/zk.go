/*
This file encapsulates the ZooKeeper and PostgreSQL connection clients for
the standalone Worker binaries, abstracting away retry logic and base node creation.
*/
package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-zookeeper/zk"
	_ "github.com/lib/pq"
)

// ZKClient wraps the ZooKeeper connection for workers
type ZKClient struct {
	conn *zk.Conn
}

// NewZKClient creates a ZooKeeper client with retry logic
func NewZKClient(hosts string) (*ZKClient, error) {
	hostList := strings.Split(hosts, ",")
	var conn *zk.Conn
	var err error

	for i := 0; i < 15; i++ {
		conn, _, err = zk.Connect(hostList, 10*time.Second)
		if err == nil {
			time.Sleep(1 * time.Second)
			log.Println("Connected to ZooKeeper")
			return &ZKClient{conn: conn}, nil
		}
		log.Printf("ZooKeeper not ready (attempt %d/15), retrying...", i+1)
		time.Sleep(3 * time.Second)
	}
	return nil, fmt.Errorf("failed to connect to ZooKeeper: %v", err)
}

// Close closes the ZK connection
func (z *ZKClient) Close() {
	z.conn.Close()
}

// EnsureBaseNodes creates the required base znodes if they don't exist
func (z *ZKClient) EnsureBaseNodes() {
	nodes := []string{"/workers", "/tasks", "/assignments", "/leader"}
	for _, node := range nodes {
		exists, _, err := z.conn.Exists(node)
		if err != nil {
			continue
		}
		if !exists {
			z.conn.Create(node, []byte{}, 0, zk.WorldACL(zk.PermAll))
		}
	}
}

// InitWorkerDB initializes the PostgreSQL connection for a worker
func InitWorkerDB(dsn string) (*sql.DB, error) {
	var db *sql.DB
	var err error

	for i := 0; i < 10; i++ {
		db, err = sql.Open("postgres", dsn)
		if err == nil {
			if pingErr := db.Ping(); pingErr == nil {
				log.Println("Worker connected to PostgreSQL")
				return db, nil
			}
		}
		log.Printf("DB not ready (attempt %d/10), retrying...", i+1)
		time.Sleep(3 * time.Second)
	}
	return nil, err
}
