import type { Task, Worker, Stat, SystemEvent, NodeInfoData } from '../services/api';
import LeaderCard from '../components/LeaderCard';
import WorkerList from '../components/WorkerList';
import TaskList from '../components/TaskList';
import TaskForm from '../components/TaskForm';
import StatsChart from '../components/StatsChart';
import EventLog from '../components/EventLog';
import type { TaskType, Priority } from '../services/api';

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
    onCreateTask: (name: string, description: string, taskType: TaskType, priority: Priority) => Promise<void>;
    onRefresh: () => void;
    onDeactivateWorker: (workerId: string) => Promise<void>;
    onActivateWorker: (workerId: string) => Promise<void>;
}

export default function Dashboard({
    leader, workers, tasks, stats, typeStats, events, loading, error,
    nodeInfo, onCreateTask, onRefresh, onDeactivateWorker, onActivateWorker
}: DashboardProps) {
    const pendingCount = tasks.filter(t => t.status === 'pending').length;
    const runningCount = tasks.filter(t => t.status === 'running').length;
    const completedCount = tasks.filter(t => t.status === 'completed').length;
    const failedCount = tasks.filter(t => t.status === 'failed').length;

    const isMaster = nodeInfo?.role === 'master' || nodeInfo?.is_leader === true;

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

                {/* Third Row: Workers + Event Log */}
                <div className="bottom-row">
                    <WorkerList
                        workers={workers}
                        leader={leader}
                        isMaster={isMaster}
                        onDeactivate={onDeactivateWorker}
                        onActivate={onActivateWorker}
                    />
                    <EventLog events={events} />
                </div>

                {/* Fourth Row: Task Table */}
                <TaskList tasks={tasks} />
            </main>
        </div>
    );
}
