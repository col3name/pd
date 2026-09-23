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

export interface PutConfigBody {
  masking?: string;
  systems?: Array<Partial<SystemInfo> & { api_key?: string | null }>;
  rules?: RuleInfo[];
  combinations?: CombinationInfo[];
}

let adminKey = '';

export function setAdminKey(k: string) {
  adminKey = k;
}

export function getAdminKey() {
  return adminKey;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(init?.headers as Record<string, string>),
  };
  if (adminKey && ['PUT', 'POST'].includes(init?.method ?? '')) {
    headers['X-Admin-Key'] = adminKey;
  }
  const res = await fetch(path, { ...init, headers });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`${res.status}: ${text.slice(0, 200)}`);
  }
  return (await res.json()) as T;
}

export const api = {
  getConfig: () => request<ConfigView>('/v1/config'),
  getRules: () => request<{ types: string[] }>('/v1/config/rules'),
  putConfig: (body: PutConfigBody) => request<{ rev: number }>('/v1/config', { method: 'PUT', body: JSON.stringify(body) }),
};