/*
This file handles ZooKeeper interactions and connection management.
It contains helper functions to read and write node data, perform leader election operations,
write heartbeat timestamps, and manage ephemeral worker znodes.
*/
package main

import (
	"fmt"
	"log"
	"path"
	"strings"
	"time"

	"github.com/go-zookeeper/zk"
)

// ZKClient wraps the ZooKeeper connection
type ZKClient struct {
	conn *zk.Conn
}

// NewZKClient creates a new ZooKeeper client with retry logic
func NewZKClient(hosts string) (*ZKClient, error) {
	hostList := strings.Split(hosts, ",")
	var conn *zk.Conn
	var err error

	for i := 0; i < 15; i++ {
		conn, _, err = zk.Connect(hostList, 10*time.Second)
		if err == nil {
			// Wait for connection to establish
			time.Sleep(1 * time.Second)
			log.Println("Connected to ZooKeeper")
			return &ZKClient{conn: conn}, nil
		}
		log.Printf("ZooKeeper not ready (attempt %d/15), retrying in 3s...", i+1)
		time.Sleep(3 * time.Second)
	}
	return nil, fmt.Errorf("failed to connect to ZooKeeper: %v", err)
}

// Close closes the ZooKeeper connection
func (z *ZKClient) Close() {
	z.conn.Close()
}

// EnsureZNodes creates the base ZooKeeper nodes if they don't exist
func (z *ZKClient) EnsureZNodes() {
	nodes := []string{"/workers", "/tasks", "/assignments", "/leader", "/heartbeats", "/nodes"}
	for _, node := range nodes {
		exists, _, err := z.conn.Exists(node)
		if err != nil {
			log.Printf("Error checking node %s: %v", node, err)
			continue
		}
		if !exists {
			_, err = z.conn.Create(node, []byte{}, 0, zk.WorldACL(zk.PermAll))
			if err != nil && err != zk.ErrNodeExists {
				log.Printf("Error creating node %s: %v", node, err)
			} else {
				log.Printf("Created ZK node: %s", node)
			}
		}
	}
}

// GetLeader returns the current leader from ZooKeeper
func (z *ZKClient) GetLeader() (string, error) {
	data, _, err := z.conn.Get("/leader")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// GetWorkers returns all active workers registered in ZooKeeper
func (z *ZKClient) GetWorkers() ([]string, error) {
	children, _, err := z.conn.Children("/workers")
	if err != nil {
		return nil, err
	}
	return children, nil
}

// CreateTaskZNode creates a znode for a task under /tasks
func (z *ZKClient) CreateTaskZNode(taskID int) error {
	taskPath := fmt.Sprintf("/tasks/task_%d", taskID)
	_, err := z.conn.Create(taskPath, []byte(fmt.Sprintf("%d", taskID)), 0, zk.WorldACL(zk.PermAll))
	if err != nil && err != zk.ErrNodeExists {
		return err
	}
	return nil
}

// DeleteTaskZNode removes a task znode
func (z *ZKClient) DeleteTaskZNode(taskID int) {
	taskPath := fmt.Sprintf("/tasks/task_%d", taskID)
	z.conn.Delete(taskPath, -1)
}

// GetTaskZNodes returns all task znodes
func (z *ZKClient) GetTaskZNodes() ([]string, error) {
	children, _, err := z.conn.Children("/tasks")
	if err != nil {
		return nil, err
	}
	return children, nil
}

// GetAssignmentZNodes returns assignments for all workers
func (z *ZKClient) GetAssignmentZNodes() (map[string][]string, error) {
	result := make(map[string][]string)
	workers, _, err := z.conn.Children("/assignments")
	if err != nil {
		return result, nil
	}

	for _, worker := range workers {
		tasks, _, err := z.conn.Children(path.Join("/assignments", worker))
		if err != nil {
			continue
		}
		result[worker] = tasks
	}
	return result, nil
}

// WatchLeader sets a watch on /leader and calls the callback when it changes
func (z *ZKClient) WatchLeader(callback func(leader string)) {
	go func() {
		for {
			data, _, ch, err := z.conn.GetW("/leader")
			if err != nil {
				log.Printf("Error watching /leader: %v", err)
				time.Sleep(2 * time.Second)
				continue
			}
			callback(string(data))
			<-ch // Wait for event
			log.Println("Leader changed, re-watching...")
		}
	}()
}

// WriteHeartbeat writes a heartbeat timestamp for a node
// Heartbeat Failure Detection:
// This function updates an ephemeral heartbeat znode with the current Unix timestamp
// to prove this node is alive and responding.
func (z *ZKClient) WriteHeartbeat(nodeID string) {
	hbPath := "/heartbeats/" + nodeID
	ts := []byte(fmt.Sprintf("%d", time.Now().UnixMilli()))
	exists, stat, err := z.conn.Exists(hbPath)
	if err != nil {
		return
	}
	if exists {
		z.conn.Set(hbPath, ts, stat.Version)
	} else {
		z.conn.Create(hbPath, ts, zk.FlagEphemeral, zk.WorldACL(zk.PermAll))
	}
}

// GetHeartbeats returns a map of nodeID -> last heartbeat unix ms
func (z *ZKClient) GetHeartbeats() map[string]int64 {
	result := make(map[string]int64)
	children, _, err := z.conn.Children("/heartbeats")
	if err != nil {
		return result
	}
	for _, child := range children {
		data, _, err := z.conn.Get("/heartbeats/" + child)
		if err != nil {
			continue
		}
		var ts int64
		fmt.Sscanf(string(data), "%d", &ts)
		result[child] = ts
	}
	return result
}

// AddDynamicWorkerZNode creates a new ephemeral sequential worker znode
// Leader Election related:
// The sequential nature assigns an incrementing number, and the lowest sequence
// can be used as the cluster leader during an election process.
func (z *ZKClient) AddDynamicWorkerZNode(workerID string) (string, error) {
	nodePath := fmt.Sprintf("/workers/%s_", workerID)
	created, err := z.conn.Create(nodePath, []byte(workerID), zk.FlagEphemeral|zk.FlagSequence, zk.WorldACL(zk.PermAll))
	if err != nil {
		return "", err
	}
	return created, nil
}

// DeleteWorkerZNode deletes a worker znode from /workers
func (z *ZKClient) DeleteWorkerZNode(znodeName string) error {
	znodePath := "/workers/" + znodeName
	return z.conn.Delete(znodePath, -1)
}
