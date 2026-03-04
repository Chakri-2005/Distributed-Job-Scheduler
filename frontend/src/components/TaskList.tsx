import { useState } from 'react';
import type { Task, LogEntry } from '../services/api';
import { fetchLogs } from '../services/api';

interface TaskListProps {
    tasks: Task[];
}

const STATUS_COLORS: Record<string, string> = {
    pending: 'status-pending',
    running: 'status-running',
    completed: 'status-completed',
    failed: 'status-failed',
};

const STATUS_ICONS: Record<string, string> = {
    pending: '⏳',
    running: '🔄',
    completed: '✅',
    failed: '❌',
};

const TYPE_ICONS: Record<string, string> = {
    batch_processing: '📦',
    email_notification: '📧',
    ai_job: '🤖',
};

const TYPE_LABELS: Record<string, string> = {
    batch_processing: 'Batch',
    email_notification: 'Email',
    ai_job: 'AI Job',
};

export default function TaskList({ tasks }: TaskListProps) {
    const [filter, setFilter] = useState<string>('all');
    const [expandedTask, setExpandedTask] = useState<number | null>(null);
    const [logs, setLogs] = useState<LogEntry[]>([]);
    const [logsLoading, setLogsLoading] = useState(false);

    const filtered = filter === 'all' ? tasks : tasks.filter(t => t.status === filter || t.task_type === filter);

    const formatTime = (iso: string) => {
        if (!iso) return '—';
        const d = new Date(iso);
        return d.toLocaleTimeString();
    };

    const formatDuration = (task: Task): string => {
        if (!task.completed_at || !task.created_at) return '—';
        const ms = new Date(task.completed_at).getTime() - new Date(task.created_at).getTime();
        return `${(ms / 1000).toFixed(1)}s`;
    };

    const toggleLogs = async (taskId: number) => {
        if (expandedTask === taskId) {
            setExpandedTask(null);
            setLogs([]);
            return;
        }
        setExpandedTask(taskId);
        setLogsLoading(true);
        try {
            const data = await fetchLogs(taskId);
            setLogs(data.logs || []);
        } catch {
            setLogs([]);
        } finally {
            setLogsLoading(false);
        }
    };

    return (
        <div className="card task-list-card">
            <div className="card-header">
                <span className="card-icon">📋</span>
                <h2 className="card-title">Tasks</h2>
                <span className="badge">{tasks.length}</span>
            </div>

            <div className="task-filters">
                {['all', 'pending', 'running', 'completed', 'failed'].map(f => (
                    <button
                        key={f}
                        className={`filter-btn ${filter === f ? 'active' : ''}`}
                        onClick={() => setFilter(f)}
                    >
                        {f === 'all' ? 'All' : `${STATUS_ICONS[f]} ${f}`}
                    </button>
                ))}
                <span className="filter-divider">|</span>
                {['batch_processing', 'email_notification', 'ai_job'].map(t => (
                    <button
                        key={t}
                        className={`filter-btn filter-type ${filter === t ? 'active' : ''}`}
                        onClick={() => setFilter(filter === t ? 'all' : t)}
                    >
                        {TYPE_ICONS[t]} {TYPE_LABELS[t]}
                    </button>
                ))}
            </div>

            <div className="task-table-wrapper">
                <table className="task-table">
                    <thead>
                        <tr>
                            <th>ID</th>
                            <th>Type</th>
                            <th>Name</th>
                            <th>Priority</th>
                            <th>Status</th>
                            <th>Worker</th>
                            <th>Retries</th>
                            <th>Created</th>
                            <th>Duration</th>
                            <th>Logs</th>
                        </tr>
                    </thead>
                    <tbody>
                        {filtered.length === 0 ? (
                            <tr>
                                <td colSpan={10} className="empty-row">
                                    {filter === 'all' ? 'No tasks yet — create one!' : `No ${filter} tasks`}
                                </td>
                            </tr>
                        ) : (
                            filtered.map(task => (
                                <>
                                    <tr key={task.id} className={`task-row ${expandedTask === task.id ? 'expanded' : ''}`}>
                                        <td className="task-id">#{task.id}</td>
                                        <td>
                                            <span className={`type-badge type-${task.task_type || 'batch_processing'}`}>
                                                {TYPE_ICONS[task.task_type] || '📦'} {TYPE_LABELS[task.task_type] || 'Batch'}
                                            </span>
                                        </td>
                                        <td className="task-name">
                                            <div className="task-name-text">{task.name}</div>
                                            {task.description && (
                                                <div className="task-desc">{task.description}</div>
                                            )}
                                        </td>
                                        <td>
                                            <span className={`priority-badge priority-${task.priority || 'medium'}`}>
                                                {task.priority === 'high' ? '🔴' : task.priority === 'low' ? '🟢' : '🟡'} {task.priority || 'medium'}
                                            </span>
                                        </td>
                                        <td>
                                            <span className={`status-badge ${STATUS_COLORS[task.status] || ''}`}>
                                                {STATUS_ICONS[task.status]} {task.status}
                                            </span>
                                        </td>
                                        <td className="task-worker">
                                            {task.assigned_worker ? (
                                                <span className="worker-chip">{task.assigned_worker}</span>
                                            ) : '—'}
                                        </td>
                                        <td className="task-retry">
                                            {task.retry_count > 0 ? (
                                                <span className="retry-badge">{task.retry_count}/{task.max_retries}</span>
                                            ) : '—'}
                                        </td>
                                        <td className="task-time">{formatTime(task.created_at)}</td>
                                        <td className="task-duration">{formatDuration(task)}</td>
                                        <td>
                                            <button
                                                className={`logs-btn ${expandedTask === task.id ? 'active' : ''}`}
                                                onClick={() => toggleLogs(task.id)}
                                                title="View execution logs"
                                            >
                                                📜
                                            </button>
                                        </td>
                                    </tr>
                                    {expandedTask === task.id && (
                                        <tr key={`logs-${task.id}`} className="logs-row">
                                            <td colSpan={10}>
                                                <div className="logs-panel">
                                                    <div className="logs-header">
                                                        <span>📜 Execution Logs — Task #{task.id}</span>
                                                    </div>
                                                    {logsLoading ? (
                                                        <div className="logs-loading">Loading logs...</div>
                                                    ) : logs.length === 0 ? (
                                                        <div className="logs-empty">No logs yet</div>
                                                    ) : (
                                                        <div className="logs-content">
                                                            {logs.map(log => (
                                                                <div key={log.id} className="log-entry">
                                                                    <span className="log-time">
                                                                        {new Date(log.created_at).toLocaleTimeString()}
                                                                    </span>
                                                                    <span className="log-worker">[{log.worker_id}]</span>
                                                                    <span className="log-message">{log.message}</span>
                                                                </div>
                                                            ))}
                                                        </div>
                                                    )}
                                                </div>
                                            </td>
                                        </tr>
                                    )}
                                </>
                            ))
                        )}
                    </tbody>
                </table>
            </div>
        </div>
    );
}
