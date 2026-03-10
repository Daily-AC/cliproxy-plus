import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { githubCopilotApi, type CopilotQuotaEntry } from '@/services/api/githubCopilot';
import { IconRefreshCw } from '@/components/ui/icons';
import styles from '@/pages/QuotaPage.module.scss';

// Known premium request multipliers for Copilot models
const PREMIUM_FACTORS: Record<string, { factor: number; note: string }> = {
  'gpt-4o': { factor: 1, note: 'Included (paid plans)' },
  'gpt-4o-mini': { factor: 0, note: 'Free' },
  'gpt-4.1': { factor: 1, note: 'Included (paid plans)' },
  'gpt-4.1-mini': { factor: 0, note: 'Free' },
  'gpt-4.5-preview': { factor: 50, note: '50x premium' },
  'o1': { factor: 1, note: '1x premium' },
  'o1-mini': { factor: 1, note: '1x premium' },
  'o1-preview': { factor: 1, note: '1x premium' },
  'o3-mini': { factor: 1, note: '1x premium' },
  'claude-3.5-sonnet': { factor: 1, note: '1x premium' },
  'claude-3.7-sonnet': { factor: 1, note: '1x premium' },
  'claude-sonnet-4': { factor: 1, note: '1x premium' },
  'claude-opus-4': { factor: 10, note: '10x premium' },
  'gemini-2.0-flash': { factor: 0.25, note: '0.25x premium' },
  'gemini-2.5-pro': { factor: 1, note: '1x premium' },
};

function getPremiumInfo(modelId: string): string {
  const lower = modelId.toLowerCase();
  for (const [key, info] of Object.entries(PREMIUM_FACTORS)) {
    if (lower.includes(key.toLowerCase())) {
      return info.note;
    }
  }
  return '1x premium';
}

export function GithubCopilotQuotaSection({ disabled }: { disabled: boolean }) {
  const { t } = useTranslation();
  const [entries, setEntries] = useState<CopilotQuotaEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [noAuth, setNoAuth] = useState(false);

  const loadQuota = useCallback(async () => {
    setLoading(true);
    try {
      const data = await githubCopilotApi.getQuota();
      if (data.status === 'no_auth') {
        setNoAuth(true);
        setEntries([]);
      } else {
        setNoAuth(false);
        setEntries(data.entries || []);
      }
    } catch {
      // silently handle
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadQuota();
  }, [loadQuota]);

  if (noAuth && !loading) return null;

  const badgeStyle = {
    backgroundColor: '#f0f0f0',
    color: '#333',
    padding: '4px 10px',
    borderRadius: '12px',
    fontSize: '12px',
    fontWeight: 600 as const,
    whiteSpace: 'nowrap' as const,
    flexShrink: 0,
  };

  return (
    <Card
      title={
        <div className={styles.titleWrapper}>
          <span>{t('quota_management.github_copilot_title')}</span>
          {entries.length > 0 && (
            <span className={styles.countBadge}>{entries.length}</span>
          )}
        </div>
      }
      extra={
        <div className={styles.headerActions}>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void loadQuota()}
            disabled={disabled || loading}
          >
            <IconRefreshCw size={14} />
            {t('common.refresh')}
          </Button>
        </div>
      }
    >
      {loading ? (
        <div className={styles.quotaMessage}>{t('common.loading')}</div>
      ) : entries.length === 0 ? (
        <div className={styles.quotaMessage}>
          {t('quota_management.github_copilot_empty')}
        </div>
      ) : (
        <div className={styles.anyRouterGrid}>
          {entries.map((entry) => (
            <div
              key={entry.id}
              className={`${styles.fileCard} ${styles.anyRouterCard}`}
              style={entry.status === 'error' ? { opacity: 0.6 } : undefined}
            >
              <div className={styles.cardHeader}>
                <span style={badgeStyle}>GitHub Copilot</span>
                <span className={styles.fileName}>{entry.label || entry.id}</span>
              </div>
              <div className={styles.quotaSection}>
                <div className={styles.quotaRow}>
                  <div className={styles.quotaRowHeader}>
                    <span className={styles.quotaModel}>
                      {t('quota_management.github_copilot_subscription')}
                    </span>
                    <span
                      className={styles.quotaPercent}
                      style={{
                        color: entry.status === 'active' ? 'var(--success-color)' : 'var(--danger-color)',
                      }}
                    >
                      {entry.status === 'active'
                        ? t('quota_management.github_copilot_active')
                        : entry.error || t('quota_management.github_copilot_inactive')}
                    </span>
                  </div>
                </div>
                {entry.models && entry.models.length > 0 && (
                  <div style={{ marginTop: '8px' }}>
                    <div
                      className={styles.quotaModel}
                      style={{ marginBottom: '6px', fontWeight: 600 }}
                    >
                      {t('quota_management.github_copilot_models')} ({entry.models.length})
                    </div>
                    <div
                      style={{
                        display: 'grid',
                        gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))',
                        gap: '4px',
                        fontSize: '12px',
                      }}
                    >
                      {entry.models.map((model) => (
                        <div
                          key={model.id}
                          style={{
                            display: 'flex',
                            justifyContent: 'space-between',
                            alignItems: 'center',
                            padding: '3px 8px',
                            backgroundColor: 'var(--bg-secondary)',
                            borderRadius: '4px',
                          }}
                        >
                          <span style={{ fontFamily: 'monospace', fontSize: '11px' }}>
                            {model.id}
                          </span>
                          <span
                            style={{
                              color: 'var(--text-secondary)',
                              fontSize: '10px',
                              marginLeft: '8px',
                              whiteSpace: 'nowrap',
                            }}
                          >
                            {getPremiumInfo(model.id)}
                          </span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}
