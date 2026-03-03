import axios from 'axios';

const API_BASE = import.meta.env.VITE_API_URL || '/api';

const api = axios.create({
  baseURL: API_BASE,
  timeout: 10000,
});

export type TaskType = 'batch_processing' | 'email_notification' | 'ai_job';

export interface Task {
  id: number;
  name: string;
  description: string;
  task_type: TaskType;
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

export const createTask = async (name: string, description: string, taskType: TaskType = 'batch_processing'): Promise<{ task: Task }> => {
  const res = await api.post('/tasks', { name, description, task_type: taskType });
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

export default api;
