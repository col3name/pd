export interface SystemInfo {
  name: string;
  api_key_set: boolean;
  enabled: boolean;
  masking: string;
  allow_unmask: boolean;
  pii: string[];
}

export interface RuleInfo {
  type: string;
  regex: string;
  priority: number;
  context: string;
  capture: string;
  keyword: string;
  confidence: number;
}

export interface CombinationInfo {
  type: string;
  requires: string[];
  window: number;
}

export interface ConfigView {
  rev: number;
  masking: string;
  systems: SystemInfo[];
  rules: RuleInfo[];
  combinations: CombinationInfo[];
  known_types: string[];
}

export interface CreateSystemBody {
  name: string;
  enabled?: boolean;
  allow_unmask?: boolean;
  masking?: string;
  pii?: string[];
}

export interface UpdateSystemBody {
  enabled?: boolean;
  allow_unmask?: boolean;
  masking?: string;
  pii?: string[];
}

let token = '';

export function setToken(t: string) {
  token = t;
}

export function getToken() {
  return token;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(init?.headers as Record<string, string>),
  };
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  const res = await fetch(path, { ...init, headers });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`${res.status}: ${text.slice(0, 200)}`);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  login: (login: string, password: string) =>
    request<{ token: string }>('/v1/auth/login', { method: 'POST', body: JSON.stringify({ login, password }) }),
  logout: () => request<void>('/v1/auth/logout', { method: 'POST' }),
  getConfig: () => request<ConfigView>('/v1/config'),
  listSystems: () => request<SystemInfo[]>('/v1/systems'),
  createSystem: (body: CreateSystemBody) =>
    request<{ name: string; access_key: string }>('/v1/systems', { method: 'POST', body: JSON.stringify(body) }),
  updateSystem: (name: string, body: UpdateSystemBody) =>
    request<{ rev: number }>(`/v1/systems/${encodeURIComponent(name)}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteSystem: (name: string) => request<void>(`/v1/systems/${encodeURIComponent(name)}`, { method: 'DELETE' }),
  regenerateKey: (name: string) =>
    request<{ access_key: string }>(`/v1/systems/${encodeURIComponent(name)}/regenerate-key`, { method: 'POST' }),
};