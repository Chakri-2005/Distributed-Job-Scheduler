import type { Worker } from '../services/api';

interface LeaderCardProps {
    leader: string;
    workers: Worker[];
}

export default function LeaderCard({ leader, workers }: LeaderCardProps) {
    const leaderWorker = workers.find(w => w.id === leader);

    return (
        <div className="card leader-card">
            <div className="card-header">
                <span className="card-icon">👑</span>
                <h2 className="card-title">Current Leader</h2>
            </div>
            <div className="leader-content">
                {leader && leader !== 'none' ? (
                    <>
                        <div className="leader-badge">
                            <span className="leader-icon">🏆</span>
                            <div className="leader-info">
                                <div className="leader-id">{leader}</div>
                                <div className="leader-status">MASTER NODE</div>
                            </div>
                        </div>
                        <div className="leader-details">
                            <div className="detail-row">
                                <span className="detail-label">Status</span>
                                <span className="detail-value active">● Active</span>
                            </div>
                            <div className="detail-row">
                                <span className="detail-label">Role</span>
                                <span className="detail-value">Scheduler</span>
                            </div>
                            {leaderWorker && (
                                <div className="detail-row">
                                    <span className="detail-label">Type</span>
                                    <span className="detail-value">Ephemeral Elected</span>
                                </div>
                            )}
                        </div>
                    </>
                ) : (
                    <div className="no-leader">
                        <span className="no-leader-icon">⏳</span>
                        <p>Electing leader...</p>
                        <p className="no-leader-sub">Workers are running election</p>
                    </div>
                )}
            </div>
        </div>
    );
}
