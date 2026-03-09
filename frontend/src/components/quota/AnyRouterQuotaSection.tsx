/**
 * AnyRouter balance quota section.
 * Unlike other quota sections that work with auth files,
 * this one directly queries the AnyRouter balance API.
 */

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
        <div className={styles.quotaSection}>
          {entries.map((entry) => (
            <div key={entry.index} className={styles.quotaRow}>
              <div className={styles.quotaRowHeader}>
                <span className={styles.quotaModel}>
                  {maskApiKey(entry.apiKey)}
                </span>
                <div className={styles.quotaMeta}>
                  {entry.loading ? (
                    <span className={styles.quotaPercent}>{t('common.loading')}</span>
                  ) : entry.balance !== null ? (
                    <span className={styles.quotaPercent}>{entry.balance}</span>
                  ) : entry.error ? (
                    <span className={styles.quotaPercent}>{t('common.error')}</span>
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
                    {t('common.refresh')}
                  </Button>
                </div>
              </div>
              {entry.error && (
                <div className={styles.quotaError}>{entry.error}</div>
              )}
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}
