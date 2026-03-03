import type { Worker } from '../services/api';

interface WorkerListProps {
    workers: Worker[];
    leader: string;
}

export default function WorkerList({ workers, leader }: WorkerListProps) {
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
                        <div key={worker.id} className={`worker-item ${worker.id === leader ? 'worker-leader' : ''}`}>
                            <div className="worker-avatar">
                                {worker.id === leader ? '👑' : '⚙️'}
                            </div>
                            <div className="worker-details">
                                <div className="worker-name">{worker.id}</div>
                                <div className="worker-role">
                                    {worker.id === leader ? 'MASTER' : 'FOLLOWER'}
                                </div>
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
