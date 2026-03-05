import type { SystemEvent } from '../services/api';

interface EventLogProps {
    events: SystemEvent[];
}

const EVENT_ICONS: Record<string, string> = {
    leader_elected: '👑',
    worker_joined: '🟢',
    worker_left: '🔴',
    worker_added: '➕',
    worker_removed: '🗑',
    worker_activated: '▶️',
    worker_deactivated: '⏸️',
    failover: '⚡',
    task_assigned: '📋',
    task_completed: '✅',
    task_failed: '❌',
    task_retried: '🔄',
    task_created: '➕',
    task_deleted: '🗑',
    heartbeat_failed: '💔',
    snapshot_taken: '📸',
    mutual_exclusion_request: '🔒',
    mutual_exclusion_enter: '🔐',
    mutual_exclusion_release: '🔓',
};

const EVENT_COLORS: Record<string, string> = {
    leader_elected: 'event-leader',
    worker_joined: 'event-join',
    worker_left: 'event-leave',
    worker_added: 'event-join',
    worker_removed: 'event-leave',
    worker_activated: 'event-join',
    worker_deactivated: 'event-leave',
    failover: 'event-failover',
    task_assigned: 'event-assign',
    task_completed: 'event-complete',
    task_failed: 'event-fail',
    task_retried: 'event-retry',
    task_created: 'event-create',
    task_deleted: 'event-fail',
    heartbeat_failed: 'event-failover',
    snapshot_taken: 'event-assign',
    mutual_exclusion_request: 'event-mutex',
    mutual_exclusion_enter: 'event-mutex',
    mutual_exclusion_release: 'event-mutex',
};

export default function EventLog({ events }: EventLogProps) {
    const formatTime = (iso: string) => {
        if (!iso) return '';
        return new Date(iso).toLocaleTimeString();
    };

    return (
        <div className="card event-log-card">
            <div className="card-header">
                <span className="card-icon">📡</span>
                <h2 className="card-title">System Events</h2>
                <span className="badge">{events.length}</span>
            </div>
            <div className="event-log-content">
                {events.length === 0 ? (
                    <div className="empty-state">
                        <span>No events yet — system events will appear here</span>
                    </div>
                ) : (
                    events.map(event => (
                        <div key={event.id} className={`event-item ${EVENT_COLORS[event.event_type] || ''}`}>
                            <span className="event-icon">
                                {EVENT_ICONS[event.event_type] || '📌'}
                            </span>
                            <div className="event-details">
                                <div className="event-message">{event.message}</div>
                                <div className="event-meta">
                                    <span className="event-source">{event.source}</span>
                                    <span className="event-time">{formatTime(event.created_at)}</span>
                                </div>
                            </div>
                        </div>
                    ))
                )}
            </div>
        </div>
    );
}
