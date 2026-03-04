import axios from 'axios';

// Determine API base URL from current window location
// Each node serves both API and frontend on the same port
const getApiBase = (): string => {
  if (import.meta.env.VITE_API_URL) {
    return import.meta.env.VITE_API_URL;
  }
  // When running behind a proxy (dev mode), use /api prefix
  // When running on a node directly, use the same origin
  const port = window.location.port;
  const host = window.location.hostname;
  if (port && ['8080', '8081', '8082', '8083', '8084'].includes(port)) {
    return `http://${host}:${port}`;
  }
  return '/api';
};

const API_BASE = getApiBase();

const api = axios.create({
  baseURL: API_BASE,
  timeout: 10000,
});

export type TaskType = 'batch_processing' | 'email_notification' | 'ai_job';
export type Priority = 'high' | 'medium' | 'low';

export interface Task {
  id: number;
  name: string;
  description: string;
  task_type: TaskType;
  priority: Priority;
  status: 'pending' | 'running' | 'completed' | 'failed';
  assigned_worker: string;
  retry_count: number;
  max_retries: number;
  created_at: string;
  completed_at?: string;
}

export interface Worker {
  id: string;
  is_leader: boolean;
  status: string;
}

export interface Stat {
  status: string;
  count: number;
}

export interface SystemEvent {
  id: number;
  event_type: string;
  source: string;
  message: string;
  created_at: string;
}

export interface LogEntry {
  id: number;
  task_id: number;
  worker_id: string;
  message: string;
  created_at: string;
}

export interface NodeInfoData {
  node_id: string;
  role: 'master' | 'slave';
  ip: string;
  port: string;
  status: string;
  is_leader: boolean;
}

export interface ClusterNodeData {
  node_id: string;
  role: string;
  ip: string;
  port: string;
  status: string;
}

export const fetchNodeInfo = async (): Promise<NodeInfoData> => {
  const res = await api.get('/node-info');
  return res.data;
};

export const fetchLeader = async (): Promise<{ leader: string }> => {
  const res = await api.get('/leader');
  return res.data;
};

export const fetchWorkers = async (): Promise<{ workers: Worker[]; count: number }> => {
  const res = await api.get('/workers');
  return res.data;
};

export const fetchTasks = async (): Promise<{ tasks: Task[]; count: number }> => {
  const res = await api.get('/tasks');
  return res.data;
};

export const createTask = async (name: string, description: string, taskType: TaskType = 'batch_processing', priority: Priority = 'medium'): Promise<{ task: Task }> => {
  const res = await api.post('/tasks', { name, description, task_type: taskType, priority });
  return res.data;
};

export const fetchStats = async (): Promise<{ stats: Stat[]; type_stats: Stat[] }> => {
  const res = await api.get('/stats');
  return res.data;
};

export const fetchAssignments = async () => {
  const res = await api.get('/assignments');
  return res.data;
};

export const fetchLogs = async (taskId: number): Promise<{ logs: LogEntry[] }> => {
  const res = await api.get(`/logs/${taskId}`);
  return res.data;
};

export const fetchEvents = async (limit: number = 50): Promise<{ events: SystemEvent[]; count: number }> => {
  const res = await api.get(`/events?limit=${limit}`);
  return res.data;
};

export const fetchClusterNodes = async (): Promise<{ nodes: ClusterNodeData[]; count: number }> => {
  const res = await api.get('/nodes');
  return res.data;
};

export const deactivateWorker = async (workerId: string): Promise<void> => {
  await api.put(`/workers/${workerId}/deactivate`);
};

export const activateWorker = async (workerId: string): Promise<void> => {
  await api.put(`/workers/${workerId}/activate`);
};

// WebSocket connection for real-time updates
export const getWebSocketUrl = (): string => {
  const port = window.location.port;
  const host = window.location.hostname;
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  
  if (port && ['8080', '8081', '8082', '8083', '8084'].includes(port)) {
    return `${protocol}//${host}:${port}/ws`;
  }
  // Fallback for dev proxy
  return `ws://${host}:8080/ws`;
};

export default api;
