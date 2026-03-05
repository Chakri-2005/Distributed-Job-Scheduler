import type { Worker } from '../services/api';

interface WorkerListProps {
    workers: Worker[];
    leader: string;
    isMaster: boolean;
    onDeactivate: (workerId: string) => Promise<void>;
    onActivate: (workerId: string) => Promise<void>;
    onAddWorker: () => Promise<void>;
    onRemoveWorker: (workerId: string) => Promise<void>;
}

// Get base ID from ZK node name: "slave1_0000000001" -> "slave1"
function getBaseId(fullId: string): string {
    const lastUnderscore = fullId.lastIndexOf('_');
    if (lastUnderscore > 0) {
        return fullId.slice(0, lastUnderscore);
    }
    return fullId;
}

// Convert internal worker node IDs to user-friendly "Worker N" labels
function getDisplayName(_workers: Worker[], index: number): string {
    return `Worker ${index + 1}`;
}


export default function WorkerList({ workers, leader, isMaster, onDeactivate, onActivate, onAddWorker, onRemoveWorker }: WorkerListProps) {
    return (
        <div className="card worker-list-card">
            <div className="card-header">
                <span className="card-icon">🖥️</span>
                <h2 className="card-title">Active Workers</h2>
                <span className="badge">{workers.length}</span>
                {isMaster && (
                    <button
                        className="add-worker-btn"
                        onClick={onAddWorker}
                        title="Add a new worker to the cluster"
                    >
                        ➕ Add Worker
                    </button>
                )}
            </div>
            <div className="worker-grid">
                {workers.length === 0 ? (
                    <div className="empty-state">
                        <span>No workers registered</span>
                    </div>
                ) : (
                    workers.map((worker, index) => {
                        const baseId = getBaseId(worker.id);
                        const displayName = getDisplayName(workers, index);
                        const isLeader = worker.id === leader;
                        const isAlive = worker.heartbeat === 'alive';
                        const isFailed = worker.heartbeat === 'failed';

                        return (
                            <div
                                key={worker.id}
                                className={`worker-item ${isLeader ? 'worker-leader' : ''} ${worker.status === 'inactive' ? 'worker-inactive' : ''} ${isFailed ? 'worker-failed' : ''}`}
                            >
                                <div className="worker-avatar">
                                    {isLeader ? '👑' : '⚙️'}
                                </div>
                                <div className="worker-details">
                                    <div className="worker-name">{displayName}</div>
                                    <div className="worker-sub">{worker.id}</div>
                                    <div className="worker-role">
                                        {isLeader ? 'MASTER' : 'FOLLOWER'}
                                        {worker.status === 'inactive' && <span className="inactive-badge"> (INACTIVE)</span>}
                                    </div>
                                    <div className="worker-heartbeat">
                                        <span className={`hb-dot ${isAlive ? 'hb-alive' : isFailed ? 'hb-failed' : 'hb-unknown'}`}>●</span>
                                        <span className="hb-label">
                                            {isAlive ? 'alive' : isFailed ? 'failed' : 'unknown'}
                                        </span>
                                    </div>
                                </div>
                                <div className="worker-actions">
                                    {isMaster && !isLeader && (
                                        <>
                                            {worker.status === 'active' ? (
                                                <button
                                                    className="worker-action-btn deactivate-btn"
                                                    onClick={() => onDeactivate(baseId)}
                                                    title="Deactivate worker"
                                                >⏸</button>
                                            ) : (
                                                <button
                                                    className="worker-action-btn activate-btn"
                                                    onClick={() => onActivate(baseId)}
                                                    title="Activate worker"
                                                >▶</button>
                                            )}
                                            <button
                                                className="worker-action-btn remove-btn"
                                                onClick={() => onRemoveWorker(baseId)}
                                                title="Remove worker from cluster"
                                            >🗑</button>
                                        </>
                                    )}
                                </div>
                                <div className={`worker-status-dot ${worker.status === 'active' ? 'active' : 'inactive'}`}></div>
                            </div>
                        );
                    })
                )}
            </div>
            <div className="card-footer">
                <div className="zk-note">
                    <span>🔗 ZooKeeper: ephemeral sequential znodes</span>
                </div>
            </div>
        </div>
    );
}
