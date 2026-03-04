import { useState, useEffect } from 'react';
import { fetchLeader, fetchWorkers, fetchTasks, fetchStats, fetchEvents, createTask } from './services/api';
import type { Task, Worker, Stat, SystemEvent, TaskType, Priority } from './services/api';
import Dashboard from './pages/Dashboard';
import './App.css';

function App() {
  const [leader, setLeader] = useState<string>('');
  const [workers, setWorkers] = useState<Worker[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [stats, setStats] = useState<Stat[]>([]);
  const [typeStats, setTypeStats] = useState<Stat[]>([]);
  const [events, setEvents] = useState<SystemEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string>('');

  const fetchAll = async () => {
    try {
      const [leaderData, workersData, tasksData, statsData, eventsData] = await Promise.all([
        fetchLeader(),
        fetchWorkers(),
        fetchTasks(),
        fetchStats(),
        fetchEvents(50),
      ]);
      setLeader(leaderData.leader || 'none');
      setWorkers(workersData.workers || []);
      setTasks(tasksData.tasks || []);
      setStats(statsData.stats || []);
      setTypeStats(statsData.type_stats || []);
      setEvents(eventsData.events || []);
      setError('');
    } catch (err: unknown) {
      setError('Failed to connect to API server. Is it running?');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchAll();
    // Poll every 2 seconds for real-time updates
    const interval = setInterval(fetchAll, 2000);
    return () => clearInterval(interval);
  }, []);

  const handleCreateTask = async (name: string, description: string, taskType: TaskType, priority: Priority) => {
    await createTask(name, description, taskType, priority);
    await fetchAll();
  };

  return (
    <div className="app">
      <Dashboard
        leader={leader}
        workers={workers}
        tasks={tasks}
        stats={stats}
        typeStats={typeStats}
        events={events}
        loading={loading}
        error={error}
        onCreateTask={handleCreateTask}
        onRefresh={fetchAll}
      />
    </div>
  );
}

export default App;
