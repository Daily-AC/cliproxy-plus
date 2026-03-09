/**
 * AnyRouter 相关 API
 */

import { apiClient } from './client';

export interface AnyRouterKeyConfig {
  apiKey: string;
  priority?: number;
  proxyUrl?: string;
  checkIn?: {
    enabled: boolean;
    userId?: string;
    sessionId?: string;
    webhookUrl?: string;
  };
}

export interface AnyRouterCheckInResponse {
  status: 'ok' | 'error';
  message?: string;
  balance?: number;
  error?: string;
}

export interface AnyRouterBalanceResponse {
  status: 'ok' | 'error';
  balance?: number;
  error?: string;
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === 'object' && !Array.isArray(value);

const extractArrayPayload = (data: unknown, key: string): unknown[] => {
  if (Array.isArray(data)) return data;
  if (!isRecord(data)) return [];
  const candidate = data[key] ?? data.items ?? data.data ?? data;
  return Array.isArray(candidate) ? candidate : [];
};

const normalizeKeyConfig = (raw: unknown): AnyRouterKeyConfig | null => {
  if (!isRecord(raw)) return null;
  const apiKey = String(raw['api-key'] ?? raw.apiKey ?? '').trim();
  if (!apiKey) return null;

  const priority = typeof raw.priority === 'number' ? raw.priority : undefined;
  const proxyUrl = String(raw['proxy-url'] ?? raw.proxyUrl ?? '').trim() || undefined;

  let checkIn: AnyRouterKeyConfig['checkIn'];
  const rawCheckIn = raw['check-in'] ?? raw.checkIn;
  if (isRecord(rawCheckIn)) {
    checkIn = {
      enabled: Boolean(rawCheckIn.enabled),
      userId: String(rawCheckIn['user-id'] ?? rawCheckIn.userId ?? '').trim() || undefined,
      sessionId: String(rawCheckIn['session-id'] ?? rawCheckIn.sessionId ?? '').trim() || undefined,
      webhookUrl: String(rawCheckIn['webhook-url'] ?? rawCheckIn.webhookUrl ?? '').trim() || undefined,
    };
  }

  return { apiKey, priority, proxyUrl, checkIn };
};

const serializeKeyConfig = (config: AnyRouterKeyConfig) => {
  const payload: Record<string, unknown> = { 'api-key': config.apiKey };
  if (config.priority !== undefined) payload.priority = config.priority;
  if (config.proxyUrl) payload['proxy-url'] = config.proxyUrl;
  if (config.checkIn) {
    const checkInPayload: Record<string, unknown> = { enabled: config.checkIn.enabled };
    if (config.checkIn.userId) checkInPayload['user-id'] = config.checkIn.userId;
    if (config.checkIn.sessionId) checkInPayload['session-id'] = config.checkIn.sessionId;
    if (config.checkIn.webhookUrl) checkInPayload['webhook-url'] = config.checkIn.webhookUrl;
    payload['check-in'] = checkInPayload;
  }
  return payload;
};

export const anyRouterApi = {
  async getKeys(): Promise<AnyRouterKeyConfig[]> {
    const data = await apiClient.get('/anyrouter-api-key');
    const list = extractArrayPayload(data, 'anyrouter-api-key');
    return list.map((item) => normalizeKeyConfig(item)).filter(Boolean) as AnyRouterKeyConfig[];
  },

  saveKeys: (configs: AnyRouterKeyConfig[]) =>
    apiClient.put('/anyrouter-api-key', configs.map((item) => serializeKeyConfig(item))),

  updateKey: (index: number, value: AnyRouterKeyConfig) =>
    apiClient.patch('/anyrouter-api-key', { index, value: serializeKeyConfig(value) }),

  deleteKey: (apiKey: string) =>
    apiClient.delete(`/anyrouter-api-key?api-key=${encodeURIComponent(apiKey)}`),

  checkIn: (index: number) =>
    apiClient.post<AnyRouterCheckInResponse>('/anyrouter/check-in', { index }),

  getBalance: (index: number) =>
    apiClient.get<AnyRouterBalanceResponse>(`/anyrouter/balance?index=${index}`),
};
