/*
This file defines the Elector struct, which manages the Leader Election process
using ZooKeeper ephemeral sequential znodes. It determines if this specific
worker node acts as the Master scheduler or a Follower worker.
*/
package main

import (
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/go-zookeeper/zk"
)

// Elector handles leader election and worker registration
type Elector struct {
	workerID string
	zkClient *ZKClient
	db       *sql.DB
	myNode   string // The ephemeral sequential node path created for this worker
}

// NewElector creates a new Elector
func NewElector(workerID string, zkClient *ZKClient, db *sql.DB) *Elector {
	return &Elector{
		workerID: workerID,
		zkClient: zkClient,
		db:       db,
	}
}

// Register creates an ephemeral sequential znode under /workers and starts election.
// Leader Election: By using zk.FlagEphemeral|zk.FlagSequence, ZooKeeper automatically
// appends an incrementing number to the znode. The node with the lowest sequence wins.
func (e *Elector) Register() error {
	// Ensure /workers exists
	e.zkClient.EnsureBaseNodes()

	// Create ephemeral sequential node: /workers/node_000000001
	nodePath := fmt.Sprintf("/workers/%s_", e.workerID)
	created, err := e.zkClient.conn.Create(
		nodePath,
		[]byte(e.workerID),
		zk.FlagEphemeral|zk.FlagSequence,
		zk.WorldACL(zk.PermAll),
	)
	if err != nil {
		return fmt.Errorf("failed to create worker znode: %v", err)
	}

	e.myNode = created
	// Extract just the node name from the full path
	parts := strings.Split(created, "/")
	nodeName := parts[len(parts)-1]
	log.Printf("Worker %s registered as: %s", e.workerID, nodeName)

	// Log worker join event
	e.logEvent("worker_joined", fmt.Sprintf("Worker %s registered in cluster as %s", e.workerID, nodeName))

	// Start election
	go e.runElection()

	return nil
}

// runElection determines if this worker is the leader
func (e *Elector) runElection() {
	for {
		isLeader, watchCh, err := e.tryBecomeLeader()
		if err != nil {
			log.Printf("Worker %s: election error: %v, retrying...", e.workerID, err)
			time.Sleep(2 * time.Second)
			continue
		}

		if isLeader {
			log.Printf("🏆 Worker %s is now MASTER/LEADER!", e.workerID)
			e.onBecomeLeader()
			// Wait until we are no longer leader (usually due to session expiration)
			// Re-run election on any event
			if watchCh != nil {
				<-watchCh
			}
		} else {
			log.Printf("Worker %s is a FOLLOWER, watching predecessor...", e.workerID)
			// Wait for the watched predecessor node to disappear
			if watchCh != nil {
				event := <-watchCh
				log.Printf("Worker %s: predecessor gone (event: %v), re-running election...", e.workerID, event.Type)
				e.logEvent("failover", fmt.Sprintf("Worker %s detected predecessor failure, re-electing...", e.workerID))
			}
		}
	}
}

// tryBecomeLeader checks if this worker has the smallest sequence number
// Leader Election (Chain-Watch): It watches the predecessor node (the node with
// the next smallest sequence number) rather than the minimum node to prevent
// the "herd effect" upon leader failure.
func (e *Elector) tryBecomeLeader() (bool, <-chan zk.Event, error) {
	children, _, err := e.zkClient.conn.Children("/workers")
	if err != nil {
		return false, nil, err
	}

	if len(children) == 0 {
		return false, nil, fmt.Errorf("no workers found")
	}

	// Extract just the node name from our full path
	myNodeName := e.myNode
	if idx := strings.LastIndex(myNodeName, "/"); idx >= 0 {
		myNodeName = myNodeName[idx+1:]
	}

	// Sort to find ordering
	sort.Strings(children)

	// Find our position
	myPos := -1
	for i, child := range children {
		if child == myNodeName {
			myPos = i
			break
		}
	}

	if myPos == -1 {
		// Our node disappeared (session expired) - re-register
		log.Printf("Worker %s: my node disappeared, re-registering...", e.workerID)
		if err := e.Register(); err != nil {
			return false, nil, err
		}
		return false, nil, fmt.Errorf("re-registered, retrying election")
	}

	if myPos == 0 {
		// We are the leader!
		return true, nil, nil
	}

	// Watch our predecessor (chain-watch pattern)
	predecessor := "/workers/" + children[myPos-1]
	_, _, watchCh, err := e.zkClient.conn.ExistsW(predecessor)
	if err != nil {
		return false, nil, err
	}
	log.Printf("Worker %s watching predecessor: %s", e.workerID, predecessor)

	return false, watchCh, nil
}

// onBecomeLeader is called when this worker becomes the leader
func (e *Elector) onBecomeLeader() {
	// Write our worker ID to /leader
	myNodeName := e.myNode
	if idx := strings.LastIndex(myNodeName, "/"); idx >= 0 {
		myNodeName = myNodeName[idx+1:]
	}

	exists, stat, err := e.zkClient.conn.Exists("/leader")
	if err != nil {
		log.Printf("Error checking /leader node: %v", err)
		return
	}

	if exists {
		_, err = e.zkClient.conn.Set("/leader", []byte(myNodeName), stat.Version)
	} else {
		_, err = e.zkClient.conn.Create("/leader", []byte(myNodeName), 0, zk.WorldACL(zk.PermAll))
	}
	if err != nil {
		log.Printf("Worker %s: failed to set /leader: %v", e.workerID, err)
	} else {
		log.Printf("Worker %s set /leader = %s", e.workerID, myNodeName)
		// Log leader election event to DB
		e.logEvent("leader_elected", fmt.Sprintf("Worker %s elected as new LEADER (node: %s)", e.workerID, myNodeName))
	}

	// As leader, start watching /tasks to assign them
	go e.watchAndAssignTasks(myNodeName)
}

// watchAndAssignTasks watches /tasks and assigns them to workers (leader only)
func (e *Elector) watchAndAssignTasks(leaderNodeName string) {
	log.Printf("Leader %s: starting task watcher...", leaderNodeName)
	for {
		// Check if we're still the leader
		leaderData, _, err := e.zkClient.conn.Get("/leader")
		if err != nil || string(leaderData) != leaderNodeName {
			log.Printf("Worker %s: no longer leader, stopping task assignment", leaderNodeName)
			return
		}

		tasks, _, ch, err := e.zkClient.conn.ChildrenW("/tasks")
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		// Assign any pending tasks
		for _, taskNode := range tasks {
			e.assignTask(taskNode, leaderNodeName)
		}

		// Wait for changes to /tasks
		select {
		case event := <-ch:
			log.Printf("Leader %s: /tasks changed (event: %v)", leaderNodeName, event.Type)
		case <-time.After(5 * time.Second):
			// Poll every 5 seconds to catch any missed events
		}
	}
}

// assignTask assigns a single task node to a worker
func (e *Elector) assignTask(taskNode, leaderNodeName string) {
	taskPath := "/tasks/" + taskNode

	// Check the task data
	data, _, err := e.zkClient.conn.Get(taskPath)
	if err != nil {
		return // Already removed
	}

	taskID := string(data)

	// Get available workers
	workers, _, err := e.zkClient.conn.Children("/workers")
	if err != nil || len(workers) == 0 {
		return
	}
	sort.Strings(workers)

	// Pick worker with fewest assignments (load balancing)
	bestWorker := ""
	minCount := -1
	for _, w := range workers {
		wPath := fmt.Sprintf("/assignments/%s", w)
		exists, _, _ := e.zkClient.conn.Exists(wPath)
		if !exists {
			e.zkClient.conn.Create(wPath, []byte{}, 0, zk.WorldACL(zk.PermAll))
			bestWorker = w
			break
		}
		tasks, _, _ := e.zkClient.conn.Children(wPath)
		cnt := len(tasks)
		if minCount == -1 || cnt < minCount {
			minCount = cnt
			bestWorker = w
		}
	}

	if bestWorker == "" {
		return
	}

	// Create assignment znode
	assignPath := fmt.Sprintf("/assignments/%s/%s", bestWorker, taskNode)
	_, err = e.zkClient.conn.Create(assignPath, []byte(taskID), 0, zk.WorldACL(zk.PermAll))
	if err != nil && err != zk.ErrNodeExists {
		log.Printf("Leader %s: failed to assign %s to %s: %v", leaderNodeName, taskNode, bestWorker, err)
		return
	}

	// Remove from /tasks to prevent duplicate assignment
	e.zkClient.conn.Delete(taskPath, -1)

	log.Printf("Leader %s: assigned task %s (id=%s) to worker %s", leaderNodeName, taskNode, taskID, bestWorker)
}

// logEvent writes a system event to the database
func (e *Elector) logEvent(eventType, message string) {
	if e.db == nil {
		return
	}
	_, err := e.db.Exec(
		`INSERT INTO system_events (event_type, source, message) VALUES ($1, $2, $3)`,
		eventType, e.workerID, message,
	)
	if err != nil {
		log.Printf("Worker %s: failed to log event: %v", e.workerID, err)
	}
}
