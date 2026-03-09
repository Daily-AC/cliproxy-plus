import { Fragment } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import iconAnyRouter from '@/assets/icons/anyrouter.svg';
import type { AnyRouterKeyConfig } from '@/services/api/anyrouter';
import { maskApiKey } from '@/utils/format';
import styles from '@/pages/AiProvidersPage.module.scss';
import { ProviderList } from '../ProviderList';

interface AnyRouterSectionProps {
  configs: AnyRouterKeyConfig[];
  loading: boolean;
  disableControls: boolean;
  isSwitching: boolean;
  onAdd: () => void;
  onEdit: (index: number) => void;
  onDelete: (index: number) => void;
}

export function AnyRouterSection({
  configs,
  loading,
  disableControls,
  isSwitching,
  onAdd,
  onEdit,
  onDelete,
}: AnyRouterSectionProps) {
  const { t } = useTranslation();
  const actionsDisabled = disableControls || loading || isSwitching;

  return (
    <>
      <Card
        title={
          <span className={styles.cardTitle}>
            <img src={iconAnyRouter} alt="" className={styles.cardTitleIcon} />
            {t('ai_providers.anyrouter_title')}
          </span>
        }
        extra={
          <Button size="sm" onClick={onAdd} disabled={actionsDisabled}>
            {t('ai_providers.anyrouter_add_button')}
          </Button>
        }
      >
        <ProviderList<AnyRouterKeyConfig>
          items={configs}
          loading={loading}
          keyField={(item) => item.apiKey}
          emptyTitle={t('ai_providers.anyrouter_empty_title')}
          emptyDescription={t('ai_providers.anyrouter_empty_desc')}
          onEdit={onEdit}
          onDelete={onDelete}
          actionsDisabled={actionsDisabled}
          renderContent={(item) => (
            <Fragment>
              <div className="item-title">{t('ai_providers.anyrouter_item_title')}</div>
              <div className={styles.fieldRow}>
                <span className={styles.fieldLabel}>{t('common.api_key')}:</span>
                <span className={styles.fieldValue}>{maskApiKey(item.apiKey)}</span>
              </div>
              {item.priority !== undefined && (
                <div className={styles.fieldRow}>
                  <span className={styles.fieldLabel}>{t('common.priority')}:</span>
                  <span className={styles.fieldValue}>{item.priority}</span>
                </div>
              )}
              {item.proxyUrl && (
                <div className={styles.fieldRow}>
                  <span className={styles.fieldLabel}>{t('common.proxy_url')}:</span>
                  <span className={styles.fieldValue}>{item.proxyUrl}</span>
                </div>
              )}
              {item.checkIn?.enabled && (
                <div className={styles.fieldRow}>
                  <span className={styles.fieldLabel}>{t('ai_providers.anyrouter_checkin_title')}:</span>
                  <span className={styles.fieldValue}>{t('ai_providers.anyrouter_checkin_enabled_label')}</span>
                </div>
              )}
            </Fragment>
          )}
        />
      </Card>
    </>
  );
}
