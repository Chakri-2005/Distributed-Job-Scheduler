import { useState } from 'react';
import type { Task, Worker, Stat, SystemEvent, NodeInfoData } from '../services/api';
import { fetchSnapshot } from '../services/api';
import LeaderCard from '../components/LeaderCard';
import WorkerList from '../components/WorkerList';
import TaskList from '../components/TaskList';
import TaskForm from '../components/TaskForm';
import StatsChart from '../components/StatsChart';
import EventLog from '../components/EventLog';
import type { TaskType, Priority, Snapshot } from '../services/api';

interface DashboardProps {
    leader: string;
    workers: Worker[];
    tasks: Task[];
    stats: Stat[];
    typeStats: Stat[];
    events: SystemEvent[];
    loading: boolean;
    error: string;
    nodeInfo: NodeInfoData | null;
    isMaster: boolean;
    onCreateTask: (name: string, description: string, taskType: TaskType, priority: Priority) => Promise<void>;
    onRefresh: () => void;
    onDeactivateWorker: (workerId: string) => Promise<void>;
    onActivateWorker: (workerId: string) => Promise<void>;
    onAddWorker: () => Promise<void>;
    onRemoveWorker: (workerId: string) => Promise<void>;
    onDeleteTask: (taskId: number) => Promise<void>;
    onDeleteAllTasks: () => Promise<void>;
}

/*
 * Dashboard Component
 * The main layout container for the application UI.
 * Orchestrates all child components (WorkerList, TaskList, etc.) and
 * provides the snapshot viewing interface for the master node.
 */
export default function Dashboard({
    leader, workers, tasks, stats, typeStats, events, loading, error,
    nodeInfo, isMaster, onCreateTask, onRefresh,
    onDeactivateWorker, onActivateWorker, onAddWorker, onRemoveWorker,
    onDeleteTask, onDeleteAllTasks
}: DashboardProps) {
    const pendingCount = tasks.filter(t => t.status === 'pending').length;
    const runningCount = tasks.filter(t => t.status === 'running').length;
    const completedCount = tasks.filter(t => t.status === 'completed').length;
    const failedCount = tasks.filter(t => t.status === 'failed').length;

    const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
    const [snapshotLoading, setSnapshotLoading] = useState(false);
    const [showSnapshot, setShowSnapshot] = useState(false);

    // handleShowSnapshot fetches and displays the distributed snapshot
    // Chandy-Lamport Distributed Snapshot:
    // When the master requests a snapshot, it queries the cluster state to form a consistent picture
    // of active workers, task counts, and completion statuses across nodes.
    const handleShowSnapshot = async () => {
        setSnapshotLoading(true);
        setShowSnapshot(true);
        try {
            const data = await fetchSnapshot();
            setSnapshot(data);
        } catch {
            setSnapshot(null);
        } finally {
            setSnapshotLoading(false);
        }
    };

    return (
        <div className="dashboard">
            <header className="header">
                <div className="header-left">
                    <div className="logo">
                        <span className="logo-icon">⚡</span>
                        <span className="logo-text">DistributedQ</span>
                    </div>
                    <span className="header-subtitle">Distributed Job Scheduler</span>
                    {nodeInfo && (
                        <span className={`node-role-badge ${isMaster ? 'role-master' : 'role-slave'}`}>
                            {isMaster ? '👑 Master' : '🔧 Slave'} • {nodeInfo.node_id} • :{nodeInfo.port}
                        </span>
                    )}
                </div>
                <div className="header-right">
                    {isMaster && (
                        <button className="snapshot-btn" onClick={handleShowSnapshot} title="Show Cluster Snapshot">
                            📊 Show Worker Status
                        </button>
                    )}
                    <div className={`connection-status ${error ? 'disconnected' : 'connected'}`}>
                        <span className="status-dot"></span>
                        {error ? 'Disconnected' : 'Live'}
                    </div>
                    <button className="refresh-btn" onClick={onRefresh} title="Refresh">
                        ↻
                    </button>
                </div>
            </header>

            {error && (
                <div className="error-banner">
                    ⚠️ {error}
                </div>
            )}

            {loading && !error && (
                <div className="loading-banner">
                    <span className="spinner"></span> Connecting to cluster...
                </div>
            )}

            {/* Chandy-Lamport Snapshot Modal */}
            {showSnapshot && (
                <div className="modal-overlay" onClick={() => setShowSnapshot(false)}>
                    <div className="modal-box" onClick={e => e.stopPropagation()}>
                        <div className="modal-header">
                            <span>📸 Cluster Snapshot (Chandy–Lamport)</span>
                            <button className="modal-close" onClick={() => setShowSnapshot(false)}>✕</button>
                        </div>
                        {snapshotLoading ? (
                            <div className="modal-loading">Capturing snapshot...</div>
                        ) : snapshot ? (
                            <div className="modal-content">
                                <div className="snapshot-grid">
                                    <div className="snapshot-item">
                                        <span className="snapshot-label">Leader</span>
                                        <span className="snapshot-value">👑 {snapshot.leader}</span>
                                    </div>
                                    <div className="snapshot-item">
                                        <span className="snapshot-label">Active Workers</span>
                                        <span className="snapshot-value">{snapshot.active_workers}</span>
                                    </div>
                                    <div className="snapshot-item">
                                        <span className="snapshot-label">Running Tasks</span>
                                        <span className="snapshot-value">{snapshot.running_tasks}</span>
                                    </div>
                                    <div className="snapshot-item">
                                        <span className="snapshot-label">Queued Tasks</span>
                                        <span className="snapshot-value">{snapshot.queued_tasks}</span>
                                    </div>
                                    <div className="snapshot-item">
                                        <span className="snapshot-label">Completed Tasks</span>
                                        <span className="snapshot-value">{snapshot.completed_tasks}</span>
                                    </div>
                                    <div className="snapshot-item">
                                        <span className="snapshot-label">Captured At</span>
                                        <span className="snapshot-value" style={{ fontSize: '0.8em' }}>{new Date(snapshot.snapshot_time).toLocaleTimeString()}</span>
                                    </div>
                                </div>
                                <div className="snapshot-workers">
                                    <h3>Task Completion per Worker</h3>
                                    {snapshot.worker_stats && snapshot.worker_stats.map((ws, i) => (
                                        <div key={ws.id} className="snapshot-worker-row">
                                            <span>Worker {i + 1} ({ws.id})</span>
                                            <span className={`hb-dot hb-${ws.status === 'alive' ? 'alive' : 'failed'}`}>●</span>
                                            <span>{ws.status}</span>
                                            <span>→ completed {ws.completed} tasks</span>
                                        </div>
                                    ))}
                                </div>
                            </div>
                        ) : (
                            <div className="modal-loading">Failed to capture snapshot</div>
                        )}
                    </div>
                </div>
            )}

            <main className="main-content">
                {/* Top Row: Stats */}
                <div className="stats-row">
                    <div className="stat-card stat-total">
                        <div className="stat-number">{tasks.length}</div>
                        <div className="stat-label">Total Tasks</div>
                    </div>
                    <div className="stat-card stat-pending">
                        <div className="stat-number">{pendingCount}</div>
                        <div className="stat-label">Pending</div>
                    </div>
                    <div className="stat-card stat-running">
                        <div className="stat-number">{runningCount}</div>
                        <div className="stat-label">Running</div>
                    </div>
                    <div className="stat-card stat-completed">
                        <div className="stat-number">{completedCount}</div>
                        <div className="stat-label">Completed</div>
                    </div>
                    <div className="stat-card stat-failed">
                        <div className="stat-number">{failedCount}</div>
                        <div className="stat-label">Failed</div>
                    </div>
                    <div className="stat-card stat-workers">
                        <div className="stat-number">{workers.length}</div>
                        <div className="stat-label">Workers</div>
                    </div>
                </div>

                {/* Second Row: Leader + Chart + Task Form */}
                <div className="middle-row">
                    <LeaderCard leader={leader} workers={workers} />
                    <StatsChart stats={stats} typeStats={typeStats} />
                    <TaskForm onSubmit={onCreateTask} />
                </div>

                {/* Third Row: Active Workers */}
                <WorkerList
                    workers={workers}
                    leader={leader}
                    isMaster={isMaster}
                    onDeactivate={onDeactivateWorker}
                    onActivate={onActivateWorker}
                    onAddWorker={onAddWorker}
                    onRemoveWorker={onRemoveWorker}
                />

                {/* Fourth Row: Tasks */}
                <TaskList
                    tasks={tasks}
                    isMaster={isMaster}
                    onDeleteTask={onDeleteTask}
                    onDeleteAllTasks={onDeleteAllTasks}
                />

                {/* Fifth Row: System Events (at bottom) */}
                <EventLog events={events} />
            </main>
        </div>
    );
}
