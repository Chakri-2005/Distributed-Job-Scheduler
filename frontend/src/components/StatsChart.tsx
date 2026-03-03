import { PieChart, Pie, Cell, Legend, ResponsiveContainer, BarChart, Bar, XAxis, YAxis, Tooltip } from 'recharts';
import type { Stat } from '../services/api';

interface StatsChartProps {
    stats: Stat[];
    typeStats?: Stat[];
}

const COLORS: Record<string, string> = {
    pending: '#f59e0b',
    running: '#3b82f6',
    completed: '#10b981',
    failed: '#ef4444',
};

const TYPE_COLORS: Record<string, string> = {
    batch_processing: '#3b82f6',
    email_notification: '#8b5cf6',
    ai_job: '#ec4899',
};

const TYPE_LABELS: Record<string, string> = {
    batch_processing: 'Batch',
    email_notification: 'Email',
    ai_job: 'AI Job',
};

const tooltipStyle = {
    background: '#1e293b',
    border: '1px solid #334155',
    borderRadius: '8px',
    color: '#e2e8f0',
};

export default function StatsChart({ stats, typeStats = [] }: StatsChartProps) {
    const chartData = stats.map(s => ({
        name: s.status,
        value: s.count,
        color: COLORS[s.status] || '#8b5cf6',
    }));

    const typeChartData = typeStats.map(s => ({
        name: TYPE_LABELS[s.status] || s.status,
        value: s.count,
        color: TYPE_COLORS[s.status] || '#8b5cf6',
    }));

    const total = stats.reduce((sum, s) => sum + s.count, 0);

    return (
        <div className="card stats-chart-card">
            <div className="card-header">
                <span className="card-icon">📊</span>
                <h2 className="card-title">Task Distribution</h2>
                <span className="badge">{total} total</span>
            </div>

            {total === 0 ? (
                <div className="empty-chart">
                    <span className="empty-chart-icon">📈</span>
                    <p>No task data yet</p>
                    <p className="empty-chart-sub">Create tasks to see statistics</p>
                </div>
            ) : (
                <div className="chart-container">
                    <ResponsiveContainer width="100%" height={140}>
                        <PieChart>
                            <Pie
                                data={chartData}
                                cx="50%"
                                cy="50%"
                                innerRadius={35}
                                outerRadius={55}
                                paddingAngle={3}
                                dataKey="value"
                            >
                                {chartData.map((entry, index) => (
                                    <Cell key={`cell-${index}`} fill={entry.color} />
                                ))}
                            </Pie>
                            <Tooltip contentStyle={tooltipStyle} />
                            <Legend
                                formatter={(value) => (
                                    <span style={{ color: '#94a3b8', fontSize: '11px' }}>{value}</span>
                                )}
                            />
                        </PieChart>
                    </ResponsiveContainer>

                    {typeChartData.length > 0 && (
                        <div className="bar-chart-wrap">
                            <div className="chart-sublabel">By Type</div>
                            <ResponsiveContainer width="100%" height={70}>
                                <BarChart data={typeChartData} margin={{ top: 4, right: 4, left: -20, bottom: 0 }}>
                                    <XAxis dataKey="name" tick={{ fill: '#94a3b8', fontSize: 10 }} />
                                    <YAxis tick={{ fill: '#94a3b8', fontSize: 10 }} />
                                    <Tooltip contentStyle={tooltipStyle} />
                                    <Bar dataKey="value" radius={[4, 4, 0, 0]}>
                                        {typeChartData.map((entry, index) => (
                                            <Cell key={`bar-${index}`} fill={entry.color} />
                                        ))}
                                    </Bar>
                                </BarChart>
                            </ResponsiveContainer>
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}
