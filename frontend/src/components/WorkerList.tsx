import type { Worker } from '../services/api';

interface WorkerListProps {
    workers: Worker[];
    leader: string;
    isMaster: boolean;
    onDeactivate: (workerId: string) => Promise<void>;
    onActivate: (workerId: string) => Promise<void>;
}

export default function WorkerList({ workers, leader, isMaster, onDeactivate, onActivate }: WorkerListProps) {
    // Extract base worker ID from the full ZK node name (e.g., "worker-1_0000000000" → "worker-1")
    const getBaseId = (fullId: string) => {
        const parts = fullId.split('_');
        return parts.slice(0, -1).join('_') || fullId;
    };

    return (
        <div className="card worker-list-card">
            <div className="card-header">
                <span className="card-icon">🖥️</span>
                <h2 className="card-title">Active Workers</h2>
                <span className="badge">{workers.length}</span>
            </div>
            <div className="worker-grid">
                {workers.length === 0 ? (
                    <div className="empty-state">
                        <span>No workers registered</span>
                    </div>
                ) : (
                    workers.map((worker) => (
                        <div key={worker.id} className={`worker-item ${worker.id === leader ? 'worker-leader' : ''} ${worker.status === 'inactive' ? 'worker-inactive' : ''}`}>
                            <div className="worker-avatar">
                                {worker.id === leader ? '👑' : '⚙️'}
                            </div>
                            <div className="worker-details">
                                <div className="worker-name">{worker.id}</div>
                                <div className="worker-role">
                                    {worker.id === leader ? 'MASTER' : 'FOLLOWER'}
                                    {worker.status === 'inactive' && <span className="inactive-badge"> (INACTIVE)</span>}
                                </div>
                            </div>
                            <div className="worker-actions">
                                {isMaster && worker.id !== leader && (
                                    worker.status === 'active' ? (
                                        <button
                                            className="worker-action-btn deactivate-btn"
                                            onClick={() => onDeactivate(getBaseId(worker.id))}
                                            title="Deactivate worker"
                                        >
                                            ⏸
                                        </button>
                                    ) : (
                                        <button
                                            className="worker-action-btn activate-btn"
                                            onClick={() => onActivate(getBaseId(worker.id))}
                                            title="Activate worker"
                                        >
                                            ▶
                                        </button>
                                    )
                                )}
                            </div>
                            <div className={`worker-status-dot ${worker.status === 'active' ? 'active' : 'inactive'}`}></div>
                        </div>
                    ))
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
