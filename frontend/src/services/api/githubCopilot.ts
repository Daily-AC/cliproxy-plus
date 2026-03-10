/**
 * GitHub Copilot quota API
 */

import { apiClient } from './client';

export interface CopilotModelInfo {
  id: string;
  name?: string;
  is_default?: boolean;
  model_picker_enabled?: boolean;
  capabilities?: string;
  rate_limit?: string;
  premium_factor?: number;
}

export interface CopilotQuotaEntry {
  id: string;
  label: string;
  status: 'active' | 'error' | string;
  subscription?: string;
  models?: CopilotModelInfo[];
  error?: string;
}

export interface CopilotQuotaResponse {
  status: 'ok' | 'no_auth' | string;
  message?: string;
  entries?: CopilotQuotaEntry[];
}

export const githubCopilotApi = {
  getQuota: () =>
    apiClient.get<CopilotQuotaResponse>('/github-copilot-quota'),
};
