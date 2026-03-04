package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/go-zookeeper/zk"
)

// NodeInfo represents a cluster node's metadata
type NodeInfo struct {
	NodeID string `json:"node_id"`
	Role   string `json:"role"` // "master" or "slave"
	IP     string `json:"ip"`
	Port   string `json:"port"`
	Status string `json:"status"` // "active" or "inactive"
}

// ClusterNode stores runtime information about the current node
type ClusterNode struct {
	NodeID      string
	Port        string
	IP          string
	Role        string // "master" or "slave"
	ZKClient    *ZKClient
	DB          *sql.DB
	MyZNode     string // Ephemeral sequential node path
	IsLeader    bool
	WorkerNodes []string
}

var clusterNode *ClusterNode

// NewClusterNode creates and initializes a cluster node
func NewClusterNode(nodeID, port, ip string, zkClient *ZKClient, db *sql.DB) *ClusterNode {
	return &ClusterNode{
		NodeID:   nodeID,
		Port:     port,
		IP:       ip,
		Role:     "slave",
		ZKClient: zkClient,
		DB:       db,
	}
}

// ensureNode creates a ZooKeeper node if it doesn't exist
func (cn *ClusterNode) ensureNode(path string) {
	exists, _, err := cn.ZKClient.conn.Exists(path)
	if err != nil {
		return
	}
	if !exists {
		cn.ZKClient.conn.Create(path, []byte{}, 0, zk.WorldACL(zk.PermAll))
	}
}

// Register registers this node in ZooKeeper and starts leader election
func (cn *ClusterNode) Register() error {
	// Ensure /nodes exists
	cn.ensureNode("/nodes")
	cn.ensureNode("/workers")
	cn.ensureNode("/tasks")
	cn.ensureNode("/assignments")

	// Ensure /leader exists
	exists, _, _ := cn.ZKClient.conn.Exists("/leader")
	if !exists {
		cn.ZKClient.conn.Create("/leader", []byte{}, 0, zk.WorldACL(zk.PermAll))
	}

	// Create ephemeral sequential node under /workers for election
	nodePath := fmt.Sprintf("/workers/%s_", cn.NodeID)
	created, err := cn.ZKClient.conn.Create(
		nodePath,
		[]byte(cn.NodeID),
		zk.FlagEphemeral|zk.FlagSequence,
		zk.WorldACL(zk.PermAll),
	)
	if err != nil {
		return fmt.Errorf("failed to create worker znode: %v", err)
	}
	cn.MyZNode = created
	log.Printf("Node %s registered as: %s", cn.NodeID, created)

	// Register node metadata under /nodes
	cn.registerNodeMetadata()

	// Log event
	CreateEvent(cn.DB, "worker_joined", cn.NodeID,
		fmt.Sprintf("Node %s joined cluster (port: %s)", cn.NodeID, cn.Port))

	// Start leader election
	go cn.runElection()

	return nil
}

// registerNodeMetadata stores node info in ZK under /nodes/<nodeID>
func (cn *ClusterNode) registerNodeMetadata() {
	nodePath := fmt.Sprintf("/nodes/%s", cn.NodeID)
	info := NodeInfo{
		NodeID: cn.NodeID,
		Role:   cn.Role,
		IP:     cn.IP,
		Port:   cn.Port,
		Status: "active",
	}
	data, _ := json.Marshal(info)

	exists, stat, _ := cn.ZKClient.conn.Exists(nodePath)
	if exists {
		cn.ZKClient.conn.Set(nodePath, data, stat.Version)
	} else {
		cn.ZKClient.conn.Create(nodePath, data, zk.FlagEphemeral, zk.WorldACL(zk.PermAll))
	}
}

// runElection performs leader election using ZK ephemeral sequential nodes
func (cn *ClusterNode) runElection() {
	for {
		isLeader, watchCh, err := cn.tryBecomeLeader()
		if err != nil {
			log.Printf("Node %s: election error: %v, retrying...", cn.NodeID, err)
			time.Sleep(2 * time.Second)
			continue
		}

		if isLeader {
			log.Printf("🏆 Node %s is now MASTER!", cn.NodeID)
			cn.IsLeader = true
			cn.Role = "master"
			cn.onBecomeLeader()

			// Update node metadata
			cn.registerNodeMetadata()

			if watchCh != nil {
				<-watchCh
			}
		} else {
			log.Printf("Node %s is a SLAVE, watching predecessor...", cn.NodeID)
			cn.IsLeader = false
			cn.Role = "slave"
			cn.registerNodeMetadata()

			if watchCh != nil {
				event := <-watchCh
				log.Printf("Node %s: predecessor gone (event: %v), re-running election...", cn.NodeID, event.Type)
				CreateEvent(cn.DB, "failover", cn.NodeID,
					fmt.Sprintf("Node %s detected predecessor failure, re-electing...", cn.NodeID))
			}
		}
	}
}

// tryBecomeLeader checks if this node has the smallest sequence number
func (cn *ClusterNode) tryBecomeLeader() (bool, <-chan zk.Event, error) {
	children, _, err := cn.ZKClient.conn.Children("/workers")
	if err != nil {
		return false, nil, err
	}

	if len(children) == 0 {
		return false, nil, fmt.Errorf("no workers found")
	}

	myNodeName := cn.MyZNode
	if idx := strings.LastIndex(myNodeName, "/"); idx >= 0 {
		myNodeName = myNodeName[idx+1:]
	}

	// Sort children
	sortStrings(children)

	myPos := -1
	for i, child := range children {
		if child == myNodeName {
			myPos = i
			break
		}
	}

	if myPos == -1 {
		log.Printf("Node %s: my node disappeared, re-registering...", cn.NodeID)
		return false, nil, fmt.Errorf("node disappeared, need re-register")
	}

	if myPos == 0 {
		return true, nil, nil
	}

	// Watch predecessor (chain-watch pattern)
	predecessor := "/workers/" + children[myPos-1]
	_, _, watchCh, err := cn.ZKClient.conn.ExistsW(predecessor)
	if err != nil {
		return false, nil, err
	}
	log.Printf("Node %s watching predecessor: %s", cn.NodeID, predecessor)

	return false, watchCh, nil
}

// onBecomeLeader is called when this node becomes the leader
func (cn *ClusterNode) onBecomeLeader() {
	myNodeName := cn.MyZNode
	if idx := strings.LastIndex(myNodeName, "/"); idx >= 0 {
		myNodeName = myNodeName[idx+1:]
	}

	exists, stat, err := cn.ZKClient.conn.Exists("/leader")
	if err != nil {
		log.Printf("Error checking /leader: %v", err)
		return
	}

	if exists {
		_, err = cn.ZKClient.conn.Set("/leader", []byte(myNodeName), stat.Version)
	} else {
		_, err = cn.ZKClient.conn.Create("/leader", []byte(myNodeName), 0, zk.WorldACL(zk.PermAll))
	}

	if err != nil {
		log.Printf("Node %s: failed to set /leader: %v", cn.NodeID, err)
	} else {
		log.Printf("Node %s set /leader = %s", cn.NodeID, myNodeName)
		CreateEvent(cn.DB, "leader_elected", cn.NodeID,
			fmt.Sprintf("Node %s elected as new MASTER (node: %s)", cn.NodeID, myNodeName))

		// Broadcast leader change via WebSocket
		if hub != nil {
			msg, _ := json.Marshal(map[string]interface{}{
				"type":   "leader_changed",
				"leader": myNodeName,
				"nodeId": cn.NodeID,
			})
			hub.Broadcast(msg)
		}
	}

	// Start task assignment as leader
	go cn.watchAndAssignTasks(myNodeName)
}

// watchAndAssignTasks watches /tasks and assigns them (leader only)
func (cn *ClusterNode) watchAndAssignTasks(leaderNodeName string) {
	log.Printf("Leader %s: starting task watcher...", leaderNodeName)
	roundRobinIdx := 0

	for {
		leaderData, _, err := cn.ZKClient.conn.Get("/leader")
		if err != nil || string(leaderData) != leaderNodeName {
			log.Printf("Node %s: no longer leader, stopping task assignment", leaderNodeName)
			return
		}

		tasks, _, ch, err := cn.ZKClient.conn.ChildrenW("/tasks")
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		for _, taskNode := range tasks {
			cn.assignTask(taskNode, leaderNodeName, &roundRobinIdx)
		}

		select {
		case event := <-ch:
			log.Printf("Leader %s: /tasks changed (event: %v)", leaderNodeName, event.Type)
		case <-time.After(5 * time.Second):
		}
	}
}

// assignTask assigns a single task to a worker using hybrid scheduling
func (cn *ClusterNode) assignTask(taskNode, leaderNodeName string, roundRobinIdx *int) {
	taskPath := "/tasks/" + taskNode

	data, _, err := cn.ZKClient.conn.Get(taskPath)
	if err != nil {
		return
	}

	taskID := string(data)

	// Get available workers (only active ones)
	workers, _, err := cn.ZKClient.conn.Children("/workers")
	if err != nil || len(workers) == 0 {
		return
	}
	sortStrings(workers)

	// Filter out inactive workers
	activeWorkers := cn.filterActiveWorkers(workers)
	if len(activeWorkers) == 0 {
		return
	}

	// Hybrid scheduling: least-loaded with round-robin fallback
	bestWorker := cn.pickWorkerHybrid(activeWorkers, roundRobinIdx)
	if bestWorker == "" {
		return
	}

	assignPath := fmt.Sprintf("/assignments/%s/%s", bestWorker, taskNode)
	workerAssignDir := fmt.Sprintf("/assignments/%s", bestWorker)

	exists, _, _ := cn.ZKClient.conn.Exists(workerAssignDir)
	if !exists {
		cn.ZKClient.conn.Create(workerAssignDir, []byte{}, 0, zk.WorldACL(zk.PermAll))
	}

	_, err = cn.ZKClient.conn.Create(assignPath, []byte(taskID), 0, zk.WorldACL(zk.PermAll))
	if err != nil && err != zk.ErrNodeExists {
		log.Printf("Failed to create assignment %s: %v", assignPath, err)
		return
	}

	// Update DB
	cn.DB.Exec(`UPDATE tasks SET status='running', assigned_worker=$1 WHERE id=$2 AND status='pending'`, bestWorker, taskID)
	cn.ZKClient.conn.Delete(taskPath, -1)

	CreateEvent(cn.DB, "task_assigned", "scheduler",
		fmt.Sprintf("Task %s assigned to %s", taskID, bestWorker))

	// Broadcast task update via WebSocket
	if hub != nil {
		msg, _ := json.Marshal(map[string]interface{}{
			"type":    "task_updated",
			"task_id": taskID,
			"status":  "running",
			"worker":  bestWorker,
		})
		hub.Broadcast(msg)
	}

	log.Printf("Leader %s: assigned task %s to worker %s", leaderNodeName, taskNode, bestWorker)
}

// filterActiveWorkers filters out workers that are inactive
func (cn *ClusterNode) filterActiveWorkers(workers []string) []string {
	var active []string
	for _, w := range workers {
		// Check if this worker's node is marked inactive
		workerBase := strings.Split(w, "_")[0]
		nodePath := fmt.Sprintf("/nodes/%s", workerBase)
		data, _, err := cn.ZKClient.conn.Get(nodePath)
		if err != nil {
			// Node exists in /workers but not in /nodes, assume active
			active = append(active, w)
			continue
		}

		var info NodeInfo
		if err := json.Unmarshal(data, &info); err != nil {
			active = append(active, w)
			continue
		}

		if info.Status == "active" {
			active = append(active, w)
		}
	}
	return active
}

// pickWorkerHybrid uses least-loaded + round-robin fallback
func (cn *ClusterNode) pickWorkerHybrid(workers []string, roundRobinIdx *int) string {
	type workerLoad struct {
		name  string
		count int
	}

	var loads []workerLoad
	for _, w := range workers {
		wPath := fmt.Sprintf("/assignments/%s", w)
		exists, _, _ := cn.ZKClient.conn.Exists(wPath)
		if !exists {
			return w // No assignments yet
		}

		tasks, _, err := cn.ZKClient.conn.Children(wPath)
		if err != nil {
			tasks = []string{}
		}
		loads = append(loads, workerLoad{name: w, count: len(tasks)})
	}

	if len(loads) == 0 {
		return ""
	}

	// Find minimum load
	minLoad := loads[0].count
	var minWorkers []string
	for _, l := range loads {
		if l.count < minLoad {
			minLoad = l.count
			minWorkers = []string{l.name}
		} else if l.count == minLoad {
			minWorkers = append(minWorkers, l.name)
		}
	}

	if len(minWorkers) == 1 {
		return minWorkers[0]
	}

	// Round-robin fallback among equally loaded workers
	idx := *roundRobinIdx % len(minWorkers)
	*roundRobinIdx++
	return minWorkers[idx]
}

// sortStrings sorts a string slice
func sortStrings(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

// GetClusterNodes returns all registered nodes from ZooKeeper
func GetClusterNodes(zkClient *ZKClient) ([]NodeInfo, error) {
	children, _, err := zkClient.conn.Children("/nodes")
	if err != nil {
		return nil, err
	}

	var nodes []NodeInfo
	for _, child := range children {
		data, _, err := zkClient.conn.Get("/nodes/" + child)
		if err != nil {
			continue
		}
		var info NodeInfo
		if err := json.Unmarshal(data, &info); err != nil {
			continue
		}
		nodes = append(nodes, info)
	}
	return nodes, nil
}

// DeactivateWorker marks a worker as inactive in ZooKeeper
func DeactivateWorker(zkClient *ZKClient, db *sql.DB, workerID string) error {
	nodePath := fmt.Sprintf("/nodes/%s", workerID)
	data, _, err := zkClient.conn.Get(nodePath)
	if err != nil {
		return fmt.Errorf("worker node not found: %v", err)
	}

	var info NodeInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return err
	}

	info.Status = "inactive"
	newData, _ := json.Marshal(info)

	_, stat, _ := zkClient.conn.Exists(nodePath)
	_, err = zkClient.conn.Set(nodePath, newData, stat.Version)
	if err != nil {
		return err
	}

	CreateEvent(db, "worker_deactivated", "master",
		fmt.Sprintf("Worker %s deactivated by master", workerID))

	// Broadcast via WebSocket
	if hub != nil {
		msg, _ := json.Marshal(map[string]interface{}{
			"type":   "worker_status_changed",
			"worker": workerID,
			"status": "inactive",
		})
		hub.Broadcast(msg)
	}

	return nil
}

// ActivateWorker marks a worker as active in ZooKeeper
func ActivateWorker(zkClient *ZKClient, db *sql.DB, workerID string) error {
	nodePath := fmt.Sprintf("/nodes/%s", workerID)
	data, _, err := zkClient.conn.Get(nodePath)
	if err != nil {
		return fmt.Errorf("worker node not found: %v", err)
	}

	var info NodeInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return err
	}

	info.Status = "active"
	newData, _ := json.Marshal(info)

	_, stat, _ := zkClient.conn.Exists(nodePath)
	_, err = zkClient.conn.Set(nodePath, newData, stat.Version)
	if err != nil {
		return err
	}

	CreateEvent(db, "worker_activated", "master",
		fmt.Sprintf("Worker %s activated by master", workerID))

	if hub != nil {
		msg, _ := json.Marshal(map[string]interface{}{
			"type":   "worker_status_changed",
			"worker": workerID,
			"status": "active",
		})
		hub.Broadcast(msg)
	}

	return nil
}

// GetLocalIP returns the machine's local network IP (prefers the real outbound IP)
func GetLocalIP() string {
	// Best method: dial a public address to find which local IP the OS routes through
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		return localAddr.IP.String()
	}

	// Fallback: scan interfaces
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "0.0.0.0"
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "0.0.0.0"
}
