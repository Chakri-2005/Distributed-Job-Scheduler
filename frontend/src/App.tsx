import { useState, useEffect, useRef, useCallback } from 'react';
import {
  fetchLeader, fetchWorkers, fetchTasks, fetchStats, fetchEvents, fetchNodeInfo,
  createTask, getWebSocketUrl, deactivateWorker, activateWorker,
  addWorker, removeWorker, deleteTask, deleteAllTasks
} from './services/api';
import type { Task, Worker, Stat, SystemEvent, TaskType, Priority, NodeInfoData } from './services/api';
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
  const [nodeInfo, setNodeInfo] = useState<NodeInfoData | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const fetchAll = useCallback(async () => {
    try {
      const [leaderData, workersData, tasksData, statsData, eventsData, nodeData] = await Promise.all([
        fetchLeader(),
        fetchWorkers(),
        fetchTasks(),
        fetchStats(),
        fetchEvents(50),
        fetchNodeInfo(),
      ]);
      setLeader(leaderData.leader || 'none');
      setWorkers(workersData.workers || []);
      setTasks(tasksData.tasks || []);
      setStats(statsData.stats || []);
      setTypeStats(statsData.type_stats || []);
      setEvents(eventsData.events || []);
      setNodeInfo(nodeData);
      setError('');
    } catch (err: unknown) {
      setError('Failed to connect to API server. Is it running?');
    } finally {
      setLoading(false);
    }
  }, []);

  const connectWebSocket = useCallback(() => {
    try {
      const wsUrl = getWebSocketUrl();
      const ws = new WebSocket(wsUrl);

      ws.onopen = () => { console.log('WebSocket connected'); };
      ws.onmessage = () => { fetchAll(); };
      ws.onclose = () => {
        console.log('WebSocket disconnected, reconnecting in 3s...');
        reconnectTimeoutRef.current = setTimeout(connectWebSocket, 3000);
      };
      ws.onerror = () => { ws.close(); };

      wsRef.current = ws;
    } catch {
      // fallback to polling
    }
  }, [fetchAll]);

  useEffect(() => {
    fetchAll();
    const interval = setInterval(fetchAll, 2000);
    connectWebSocket();
    return () => {
      clearInterval(interval);
      if (wsRef.current) wsRef.current.close();
      if (reconnectTimeoutRef.current) clearTimeout(reconnectTimeoutRef.current);
    };
  }, [fetchAll, connectWebSocket]);

  const handleCreateTask = async (name: string, description: string, taskType: TaskType, priority: Priority) => {
    await createTask(name, description, taskType, priority);
    await fetchAll();
  };

  const handleDeactivateWorker = async (workerId: string) => {
    try { await deactivateWorker(workerId); await fetchAll(); } catch { /* slave */ }
  };

  const handleActivateWorker = async (workerId: string) => {
    try { await activateWorker(workerId); await fetchAll(); } catch { /* slave */ }
  };

  const handleAddWorker = async () => {
    try { await addWorker(); await fetchAll(); } catch { /* slave */ }
  };

  const handleRemoveWorker = async (workerId: string) => {
    try { await removeWorker(workerId); await fetchAll(); } catch { /* slave */ }
  };

  const handleDeleteTask = async (taskId: number) => {
    try { await deleteTask(taskId); await fetchAll(); } catch { /* slave */ }
  };

  const handleDeleteAllTasks = async () => {
    try { await deleteAllTasks(); await fetchAll(); } catch { /* slave */ }
  };

  const isMaster = nodeInfo?.role === 'master' || nodeInfo?.is_leader === true;

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
        nodeInfo={nodeInfo}
        isMaster={isMaster}
        onCreateTask={handleCreateTask}
        onRefresh={fetchAll}
        onDeactivateWorker={handleDeactivateWorker}
        onActivateWorker={handleActivateWorker}
        onAddWorker={handleAddWorker}
        onRemoveWorker={handleRemoveWorker}
        onDeleteTask={handleDeleteTask}
        onDeleteAllTasks={handleDeleteAllTasks}
      />
    </div>
  );
}

export default App;
