package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
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
	mutex       *MutualExclusionState
}

// MutualExclusionState implements simplified Ricart-Agrawala
type MutualExclusionState struct {
	mu           sync.Mutex
	inCS         bool
	timestamp    int64
	requestQueue []chan struct{}
}

func newMutualExclusionState() *MutualExclusionState {
	return &MutualExclusionState{}
}

// AcquireCS acquires the simulated critical section (simplified single-node variant for display purposes)
func (m *MutualExclusionState) AcquireCS(reason, nodeID string, db *sql.DB) {
	m.mu.Lock()
	m.inCS = true
	m.timestamp = time.Now().UnixMilli()
	m.mu.Unlock()
	CreateEvent(db, "mutual_exclusion_request",
		nodeID, fmt.Sprintf("%s requesting critical section (clock: %d)", nodeID, m.timestamp))
	CreateEvent(db, "mutual_exclusion_enter",
		nodeID, fmt.Sprintf("%s entered critical section — %s", nodeID, reason))
	if hub != nil {
		msg, _ := json.Marshal(map[string]interface{}{
			"type":      "mutual_exclusion",
			"action":    "enter",
			"node":      nodeID,
			"reason":    reason,
			"timestamp": m.timestamp,
		})
		hub.Broadcast(msg)
	}
}

// ReleaseCS releases the critical section
func (m *MutualExclusionState) ReleaseCS(reason, nodeID string, db *sql.DB) {
	m.mu.Lock()
	m.inCS = false
	m.mu.Unlock()
	CreateEvent(db, "mutual_exclusion_release",
		nodeID, fmt.Sprintf("%s released critical section — %s", nodeID, reason))
	if hub != nil {
		msg, _ := json.Marshal(map[string]interface{}{
			"type":   "mutual_exclusion",
			"action": "release",
			"node":   nodeID,
			"reason": reason,
		})
		hub.Broadcast(msg)
	}
}

var clusterNode *ClusterNode

// NewClusterNode creates and initializes a cluster node
func NewClusterNode(nodeID, port, ip string, zkClient *ZKClient, db *sql.DB) *ClusterNode {
	role := "slave"
	if port == "8080" {
		role = "master"
	}
	return &ClusterNode{
		NodeID:   nodeID,
		Port:     port,
		IP:       ip,
		Role:     role,
		ZKClient: zkClient,
		DB:       db,
		mutex:    newMutualExclusionState(),
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
	// Ensure base znodes exist
	cn.ensureNode("/nodes")
	cn.ensureNode("/workers")
	cn.ensureNode("/tasks")
	cn.ensureNode("/assignments")
	cn.ensureNode("/heartbeats")

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

	// Start heartbeat
	go cn.startHeartbeat()

	// Start static role assignment and leader duties
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

// runElection performs static role initialization
func (cn *ClusterNode) runElection() {
	if cn.Role == "master" {
		log.Printf("🏆 Node %s is statically assigned as MASTER!", cn.NodeID)
		cn.IsLeader = true
		cn.onBecomeLeader()
		// Update node metadata
		cn.registerNodeMetadata()
		// Master watches heartbeats
		go cn.watchHeartbeats()
		// Master blocks forever
		select {}
	} else {
		log.Printf("Node %s is statically assigned as a SLAVE", cn.NodeID)
		cn.IsLeader = false
		cn.registerNodeMetadata()
		// Slaves block forever
		select {}
	}
}

// startHeartbeat periodically writes a heartbeat to ZK
func (cn *ClusterNode) startHeartbeat() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		cn.ZKClient.WriteHeartbeat(cn.NodeID)
	}
}

// watchHeartbeats watches all workers' heartbeats and marks failures (master only)
func (cn *ClusterNode) watchHeartbeats() {
	log.Printf("Master %s: starting heartbeat watcher...", cn.NodeID)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		heartbeats := cn.ZKClient.GetHeartbeats()
		now := time.Now().UnixMilli()
		workers, _, _ := cn.ZKClient.conn.Children("/workers")
		for _, w := range workers {
			workerBase := w
			// strip sequence suffix
			for i := len(w) - 1; i >= 0; i-- {
				if w[i] == '_' {
					workerBase = w[:i]
					break
				}
			}
			lastBeat, ok := heartbeats[workerBase]
			if !ok {
				continue
			}
			// If more than 15 seconds since last heartbeat, mark failed
			if now-lastBeat > 15000 {
				log.Printf("⚠️ Heartbeat failure detected for worker: %s", workerBase)
				cn.handleHeartbeatFailure(workerBase)
			}
		}
	}
}

// handleHeartbeatFailure marks the worker inactive and reassigns its tasks
func (cn *ClusterNode) handleHeartbeatFailure(workerBase string) {
	nodePath := fmt.Sprintf("/nodes/%s", workerBase)
	data, _, err := cn.ZKClient.conn.Get(nodePath)
	if err != nil {
		return
	}
	var info NodeInfo
	if json.Unmarshal(data, &info) != nil {
		return
	}
	if info.Status == "inactive" {
		return // Already marked
	}
	info.Status = "inactive"
	newData, _ := json.Marshal(info)
	_, stat, _ := cn.ZKClient.conn.Exists(nodePath)
	cn.ZKClient.conn.Set(nodePath, newData, stat.Version)

	CreateEvent(cn.DB, "heartbeat_failed", cn.NodeID,
		fmt.Sprintf("Worker %s failed heartbeat — marked inactive, reassigning tasks", workerBase))

	// Reassign running tasks to other workers
	cn.DB.Exec(`UPDATE tasks SET status='pending', assigned_worker='' WHERE assigned_worker=$1 AND status='running'`, workerBase)
	cn.DB.Exec(`DELETE FROM task_logs WHERE worker_id=$1 AND created_at > NOW() - INTERVAL '1 minute'`, workerBase)

	if hub != nil {
		msg, _ := json.Marshal(map[string]interface{}{
			"type":   "heartbeat_failed",
			"worker": workerBase,
		})
		hub.Broadcast(msg)
	}
}

// onBecomeLeader is called when this node becomes the leader
func (cn *ClusterNode) onBecomeLeader() {
	myNodeName := cn.MyZNode
	for i := len(myNodeName) - 1; i >= 0; i-- {
		if myNodeName[i] == '/' {
			myNodeName = myNodeName[i+1:]
			break
		}
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
		workerBase := w
		for i := len(w) - 1; i >= 0; i-- {
			if w[i] == '_' {
				workerBase = w[:i]
				break
			}
		}
		nodePath := fmt.Sprintf("/nodes/%s", workerBase)
		data, _, err := cn.ZKClient.conn.Get(nodePath)
		if err != nil {
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

// AddDynamicWorker adds a new ephemeral worker node to the cluster (master only)
func AddDynamicWorker(zkClient *ZKClient, db *sql.DB, nodeIP string) (string, error) {
	// Find next worker number
	children, _, _ := zkClient.conn.Children("/workers")
	newID := fmt.Sprintf("worker%d", len(children)+1)

	// Create ephemeral sequential znode
	znodePath, err := zkClient.AddDynamicWorkerZNode(newID)
	if err != nil {
		return "", fmt.Errorf("failed to create worker znode: %v", err)
	}

	// Extract just the node name from the full path
	znodeName := znodePath
	for i := len(znodePath) - 1; i >= 0; i-- {
		if znodePath[i] == '/' {
			znodeName = znodePath[i+1:]
			break
		}
	}

	// Register node metadata
	nodePath := fmt.Sprintf("/nodes/%s", newID)
	info := NodeInfo{
		NodeID: newID,
		Role:   "slave",
		IP:     nodeIP,
		Port:   "dynamic",
		Status: "active",
	}
	data, _ := json.Marshal(info)
	zkClient.conn.Create(nodePath, data, 0, zk.WorldACL(zk.PermAll))

	CreateEvent(db, "worker_added", "master",
		fmt.Sprintf("Dynamic worker %s added to cluster (znode: %s)", newID, znodeName))

	if hub != nil {
		msg, _ := json.Marshal(map[string]interface{}{
			"type":   "worker_added",
			"worker": newID,
			"znode":  znodeName,
		})
		hub.Broadcast(msg)
	}

	return newID, nil
}

// RemoveDynamicWorker removes a worker from the cluster (master only)
func RemoveDynamicWorker(zkClient *ZKClient, db *sql.DB, workerID string) error {
	// Find the znode for this worker
	children, _, err := zkClient.conn.Children("/workers")
	if err != nil {
		return err
	}

	var znodeName string
	for _, child := range children {
		// Match by prefix
		if len(child) > len(workerID) && child[:len(workerID)] == workerID {
			znodeName = child
			break
		}
	}

	if znodeName != "" {
		zkClient.DeleteWorkerZNode(znodeName)
	}

	// Remove from /nodes
	nodePath := fmt.Sprintf("/nodes/%s", workerID)
	exists, stat, _ := zkClient.conn.Exists(nodePath)
	if exists {
		zkClient.conn.Delete(nodePath, stat.Version)
	}

	// Reassign running tasks
	db.Exec(`UPDATE tasks SET status='pending', assigned_worker='' WHERE assigned_worker=$1 AND status='running'`, workerID)

	CreateEvent(db, "worker_removed", "master",
		fmt.Sprintf("Worker %s removed from cluster by master", workerID))

	if hub != nil {
		msg, _ := json.Marshal(map[string]interface{}{
			"type":   "worker_removed",
			"worker": workerID,
		})
		hub.Broadcast(msg)
	}

	return nil
}

// GetClusterSnapshot builds a Chandy-Lamport style cluster snapshot
func GetClusterSnapshot(zkClient *ZKClient, db *sql.DB) map[string]interface{} {
	leader, _ := zkClient.GetLeader()
	workers, _ := zkClient.GetWorkers()
	heartbeats := zkClient.GetHeartbeats()
	now := time.Now().UnixMilli()

	type WorkerStat struct {
		ID        string `json:"id"`
		Completed int    `json:"completed"`
		Status    string `json:"status"`
	}

	activeCount := 0
	workerStats := []WorkerStat{}
	for _, w := range workers {
		workerBase := w
		for i := len(w) - 1; i >= 0; i-- {
			if w[i] == '_' {
				workerBase = w[:i]
				break
			}
		}

		// Check heartbeat status
		status := "alive"
		if lastBeat, ok := heartbeats[workerBase]; ok {
			if now-lastBeat > 15000 {
				status = "failed"
			}
		}

		// Count completed tasks
		var completed int
		db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE assigned_worker=$1 AND status='completed'`, workerBase).Scan(&completed)

		workerStats = append(workerStats, WorkerStat{
			ID:        workerBase,
			Completed: completed,
			Status:    status,
		})
		activeCount++
	}

	var running, queued, completed int
	db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE status='running'`).Scan(&running)
	db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE status='pending'`).Scan(&queued)
	db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE status='completed'`).Scan(&completed)

	return map[string]interface{}{
		"leader":          leader,
		"active_workers":  activeCount,
		"running_tasks":   running,
		"queued_tasks":    queued,
		"completed_tasks": completed,
		"worker_stats":    workerStats,
		"snapshot_time":   time.Now().Format(time.RFC3339),
	}
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

// GetWorkerDisplayName converts internal node ID to user-friendly "Worker N" format
func GetWorkerDisplayName(workerID string, index int) string {
	return "Worker " + strconv.Itoa(index+1)
}
