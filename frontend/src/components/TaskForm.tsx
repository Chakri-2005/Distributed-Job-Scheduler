/*
 * TaskForm Component
 * Renders the form interface allowing users to create new jobs.
 * Users can specify the task name, description, exact type (batch, email, AI),
 * and prioritization. Also includes one-click "Quick Tasks" shortcuts.
 */
import { useState } from 'react';
import type { TaskType, Priority } from '../services/api';

interface TaskFormProps {
    onSubmit: (name: string, description: string, taskType: TaskType, priority: Priority) => Promise<void>;
}

const TASK_TYPES: { value: TaskType; label: string; icon: string; description: string }[] = [
    { value: 'batch_processing', label: 'Batch Processing', icon: '📦', description: 'Process files, data records, or documents in bulk' },
    { value: 'email_notification', label: 'Email Notification', icon: '📧', description: 'Send email alerts to multiple recipients' },
    { value: 'ai_job', label: 'AI Job', icon: '🤖', description: 'ML model training, inference, or data analysis' },
];

export default function TaskForm({ onSubmit }: TaskFormProps) {
    const [name, setName] = useState('');
    const [description, setDescription] = useState('');
    const [taskType, setTaskType] = useState<TaskType>('batch_processing');
    const [priority, setPriority] = useState<Priority>('medium');
    const [loading, setLoading] = useState(false);
    const [success, setSuccess] = useState(false);
    const [error, setError] = useState('');

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!name.trim()) return;

        setLoading(true);
        setError('');
        setSuccess(false);

        try {
            await onSubmit(name.trim(), description.trim(), taskType, priority);
            setName('');
            setDescription('');
            setSuccess(true);
            setTimeout(() => setSuccess(false), 3000);
        } catch {
            setError('Failed to create task. Is the API running?');
        } finally {
            setLoading(false);
        }
    };

    const quickTasks: { name: string; type: TaskType; priority: Priority }[] = [
        { name: 'Process CSV Data', type: 'batch_processing', priority: 'medium' },
        { name: 'Send Weekly Report', type: 'email_notification', priority: 'low' },
        { name: 'Train Classifier', type: 'ai_job', priority: 'high' },
        { name: 'Backup Database', type: 'batch_processing', priority: 'high' },
        { name: 'Alert Admins', type: 'email_notification', priority: 'high' },
        { name: 'Run Inference', type: 'ai_job', priority: 'medium' },
    ];

    return (
        <div className="card task-form-card">
            <div className="card-header">
                <span className="card-icon">➕</span>
                <h2 className="card-title">Create Task</h2>
            </div>

            <form onSubmit={handleSubmit} className="task-form">
                <div className="form-group">
                    <label className="form-label">Task Type *</label>
                    <div className="task-type-selector">
                        {TASK_TYPES.map(tt => (
                            <button
                                key={tt.value}
                                type="button"
                                className={`type-option ${taskType === tt.value ? 'active' : ''}`}
                                onClick={() => setTaskType(tt.value)}
                                disabled={loading}
                            >
                                <span className="type-icon">{tt.icon}</span>
                                <span className="type-label">{tt.label}</span>
                            </button>
                        ))}
                    </div>
                </div>

                <div className="form-group">
                    <label className="form-label">Priority</label>
                    <div className="priority-selector">
                        {([['high', '🔴', 'High'], ['medium', '🟡', 'Medium'], ['low', '🟢', 'Low']] as const).map(([val, icon, label]) => (
                            <button
                                key={val}
                                type="button"
                                className={`priority-option priority-${val} ${priority === val ? 'active' : ''}`}
                                onClick={() => setPriority(val)}
                                disabled={loading}
                            >
                                {icon} {label}
                            </button>
                        ))}
                    </div>
                </div>

                <div className="form-group">
                    <label className="form-label">Task Name *</label>
                    <input
                        type="text"
                        className="form-input"
                        placeholder={`e.g. ${TASK_TYPES.find(t => t.value === taskType)?.description || 'Enter task name'}`}
                        value={name}
                        onChange={e => setName(e.target.value)}
                        disabled={loading}
                        required
                    />
                </div>

                <div className="form-group">
                    <label className="form-label">Description</label>
                    <textarea
                        className="form-input form-textarea"
                        placeholder="Optional task details..."
                        value={description}
                        onChange={e => setDescription(e.target.value)}
                        disabled={loading}
                        rows={2}
                    />
                </div>

                <div className="quick-tasks">
                    <span className="quick-label">Quick:</span>
                    {quickTasks.map(qt => (
                        <button
                            key={qt.name}
                            type="button"
                            className={`quick-btn ${qt.type === 'email_notification' ? 'quick-email' : qt.type === 'ai_job' ? 'quick-ai' : ''}`}
                            onClick={() => { setName(qt.name); setTaskType(qt.type); setPriority(qt.priority); }}
                            disabled={loading}
                        >
                            {qt.type === 'batch_processing' ? '📦' : qt.type === 'email_notification' ? '📧' : '🤖'} {qt.name}
                        </button>
                    ))}
                </div>

                {error && <div className="form-error">⚠️ {error}</div>}
                {success && <div className="form-success">✅ Task created and queued!</div>}

                <button
                    type="submit"
                    className={`submit-btn ${loading ? 'loading' : ''}`}
                    disabled={loading || !name.trim()}
                >
                    {loading ? (
                        <><span className="spinner-sm"></span> Creating...</>
                    ) : (
                        '🚀 Create Task'
                    )}
                </button>
            </form>
        </div>
    );
}
