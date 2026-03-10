import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { anyRouterApi, type AnyRouterKeyConfig } from '@/services/api/anyrouter';
import { maskApiKey } from '@/utils/format';
import { IconRefreshCw } from '@/components/ui/icons';
import styles from '@/pages/QuotaPage.module.scss';

interface BalanceEntry {
  index: number;
  apiKey: string;
  label?: string;
  balance: number | null;
  loading: boolean;
  error?: string;
}

export function AnyRouterQuotaSection({ disabled }: { disabled: boolean }) {
  const { t } = useTranslation();
  const [keys, setKeys] = useState<AnyRouterKeyConfig[]>([]);
  const [entries, setEntries] = useState<BalanceEntry[]>([]);
  const [loading, setLoading] = useState(true);

  const loadKeys = useCallback(async () => {
    setLoading(true);
    try {
      const data = await anyRouterApi.getKeys();
      setKeys(data);
      setEntries(data.map((key, index) => ({
        index,
        apiKey: key.apiKey,
        label: key.label,
        balance: null,
        loading: false,
      })));
    } catch {
      // silently handle
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadKeys();
  }, [loadKeys]);

  const refreshBalance = useCallback(async (index: number) => {
    setEntries((prev) =>
      prev.map((e) => (e.index === index ? { ...e, loading: true, error: undefined } : e))
    );
    try {
      const res = await anyRouterApi.getBalance(index);
      setEntries((prev) =>
        prev.map((e) =>
          e.index === index
            ? { ...e, balance: res.balance ?? null, loading: false }
            : e
        )
      );
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : '';
      setEntries((prev) =>
        prev.map((e) =>
          e.index === index ? { ...e, loading: false, error: message } : e
        )
      );
    }
  }, []);

  const refreshAll = useCallback(async () => {
    for (const entry of entries) {
      await refreshBalance(entry.index);
    }
  }, [entries, refreshBalance]);

  if (keys.length === 0 && !loading) return null;

  const anyRouterBadgeStyle = {
    backgroundColor: '#ede9fe',
    color: '#7c3aed',
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
          <span>{t('quota_management.anyrouter_title')}</span>
          <span className={styles.countBadge}>{keys.length}</span>
        </div>
      }
      extra={
        <div className={styles.headerActions}>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void refreshAll()}
            disabled={disabled || loading || entries.length === 0}
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
          {t('quota_management.anyrouter_empty')}
        </div>
      ) : (
        <div className={styles.anyRouterGrid}>
          {entries.map((entry, idx) => {
            const key = keys[idx];
            const isDisabled = key?.enabled === false;
            return (
              <div
                key={entry.index}
                className={`${styles.fileCard} ${styles.anyRouterCard}`}
                style={isDisabled ? { opacity: 0.6 } : undefined}
              >
                <div className={styles.cardHeader}>
                  <span style={anyRouterBadgeStyle}>AnyRouter</span>
                  <span className={styles.fileName}>
                    {entry.label || maskApiKey(entry.apiKey)}
                  </span>
                </div>
                <div className={styles.quotaSection}>
                  <div className={styles.quotaRow}>
                    <div className={styles.quotaRowHeader}>
                      <span className={styles.quotaModel}>
                        {t('ai_providers.anyrouter_balance_label')}
                      </span>
                      <div className={styles.quotaMeta}>
                        {entry.loading ? (
                          <span className={styles.quotaPercent}>{t('common.loading')}</span>
                        ) : entry.balance !== null ? (
                          <span className={styles.quotaPercent}>{entry.balance.toFixed(2)}</span>
                        ) : entry.error ? (
                          <span className={styles.quotaPercent} style={{ color: 'var(--danger-color)' }}>
                            {t('common.error')}
                          </span>
                        ) : (
                          <span className={styles.quotaPercent}>--</span>
                        )}
                        <Button
                          variant="secondary"
                          size="sm"
                          onClick={() => void refreshBalance(entry.index)}
                          disabled={disabled || entry.loading}
                          style={{ padding: '2px 8px', fontSize: '11px' }}
                        >
                          <IconRefreshCw size={12} />
                        </Button>
                      </div>
                    </div>
                  </div>
                  {entry.error && (
                    <div className={styles.quotaError}>{entry.error}</div>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </Card>
  );
}
