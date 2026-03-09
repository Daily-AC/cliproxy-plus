import { useCallback, useEffect, useMemo, useState } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import { useEdgeSwipeBack } from '@/hooks/useEdgeSwipeBack';
import { SecondaryScreenShell } from '@/components/common/SecondaryScreenShell';
import { anyRouterApi, type AnyRouterKeyConfig } from '@/services/api/anyrouter';
import { useAuthStore, useNotificationStore } from '@/stores';
import layoutStyles from './AiProvidersEditLayout.module.scss';
import styles from './AiProvidersPage.module.scss';

type LocationState = { fromAiProviders?: boolean } | null;

const ANYROUTER_MODELS = [
  'claude-opus-4-6',
  'claude-sonnet-4-6',
  'claude-sonnet-4-5-20250929',
  'claude-haiku-4-5-20251001',
  'claude-opus-4-5-20251101',
  'claude-opus-4-1-20250805',
  'claude-sonnet-4-20250514',
  'claude-opus-4-20250514',
];

interface FormState {
  apiKey: string;
  priority?: number;
  proxyUrl: string;
  checkInEnabled: boolean;
  checkInUserId: string;
  checkInSessionId: string;
  checkInWebhookUrl: string;
}

const buildEmptyForm = (): FormState => ({
  apiKey: '',
  priority: undefined,
  proxyUrl: '',
  checkInEnabled: false,
  checkInUserId: '',
  checkInSessionId: '',
  checkInWebhookUrl: '',
});

const parseIndexParam = (value: string | undefined) => {
  if (!value) return null;
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : null;
};

const getErrorMessage = (err: unknown) => {
  if (err instanceof Error) return err.message;
  if (typeof err === 'string') return err;
  return '';
};

export function AiProvidersAnyRouterEditPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const params = useParams<{ index?: string }>();

  const { showNotification } = useNotificationStore();
  const connectionStatus = useAuthStore((state) => state.connectionStatus);
  const disableControls = connectionStatus !== 'connected';

  const [configs, setConfigs] = useState<AnyRouterKeyConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [form, setForm] = useState<FormState>(() => buildEmptyForm());
  const [checkInLoading, setCheckInLoading] = useState(false);
  const [checkInResult, setCheckInResult] = useState<string | null>(null);
  const [balanceLoading, setBalanceLoading] = useState(false);
  const [balance, setBalance] = useState<number | null>(null);

  const hasIndexParam = typeof params.index === 'string';
  const editIndex = useMemo(() => parseIndexParam(params.index), [params.index]);
  const invalidIndexParam = hasIndexParam && editIndex === null;

  const initialData = useMemo(() => {
    if (editIndex === null) return undefined;
    return configs[editIndex];
  }, [configs, editIndex]);

  const invalidIndex = editIndex !== null && !initialData;

  const title = editIndex !== null
    ? t('ai_providers.anyrouter_edit_title')
    : t('ai_providers.anyrouter_add_title');

  const handleBack = useCallback(() => {
    const state = location.state as LocationState;
    if (state?.fromAiProviders) {
      navigate(-1);
      return;
    }
    navigate('/ai-providers', { replace: true });
  }, [location.state, navigate]);

  const swipeRef = useEdgeSwipeBack({ onBack: handleBack });

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        handleBack();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [handleBack]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);

    anyRouterApi.getKeys()
      .then((data) => {
        if (cancelled) return;
        setConfigs(data);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        const message = getErrorMessage(err) || t('notification.refresh_failed');
        showNotification(message, 'error');
      })
      .finally(() => {
        if (cancelled) return;
        setLoading(false);
      });

    return () => { cancelled = true; };
  }, [showNotification, t]);

  useEffect(() => {
    if (loading) return;

    if (initialData) {
      setForm({
        apiKey: initialData.apiKey,
        priority: initialData.priority,
        proxyUrl: initialData.proxyUrl ?? '',
        checkInEnabled: initialData.checkIn?.enabled ?? false,
        checkInUserId: initialData.checkIn?.userId ?? '',
        checkInSessionId: initialData.checkIn?.sessionId ?? '',
        checkInWebhookUrl: initialData.checkIn?.webhookUrl ?? '',
      });
      return;
    }
    setForm(buildEmptyForm());
  }, [initialData, loading]);

  // Load balance when editing existing key
  useEffect(() => {
    if (editIndex === null || loading) return;
    setBalanceLoading(true);
    anyRouterApi.getBalance(editIndex)
      .then((res) => {
        if (res.balance !== undefined) {
          setBalance(res.balance);
        }
      })
      .catch(() => {
        // silently ignore balance errors
      })
      .finally(() => setBalanceLoading(false));
  }, [editIndex, loading]);

  const canSave = !disableControls && !saving && !loading && !invalidIndexParam && !invalidIndex;

  const buildConfig = (): AnyRouterKeyConfig => ({
    apiKey: form.apiKey.trim(),
    priority: form.priority,
    proxyUrl: form.proxyUrl.trim() || undefined,
    checkIn: form.checkInEnabled
      ? {
          enabled: true,
          userId: form.checkInUserId.trim() || undefined,
          sessionId: form.checkInSessionId.trim() || undefined,
          webhookUrl: form.checkInWebhookUrl.trim() || undefined,
        }
      : undefined,
  });

  const handleSave = async () => {
    if (!canSave) return;

    const apiKey = form.apiKey.trim();
    if (!apiKey) {
      showNotification(t('ai_providers.anyrouter_key_required'), 'warning');
      return;
    }

    setSaving(true);
    try {
      const config = buildConfig();

      if (editIndex !== null) {
        const nextConfigs = configs.map((item, idx) => (idx === editIndex ? config : item));
        await anyRouterApi.saveKeys(nextConfigs);
        showNotification(t('notification.save_success'), 'success');
      } else {
        const nextConfigs = [...configs, config];
        await anyRouterApi.saveKeys(nextConfigs);
        showNotification(t('notification.save_success'), 'success');
      }

      handleBack();
    } catch (err: unknown) {
      const message = getErrorMessage(err);
      showNotification(`${t('notification.save_failed')}: ${message}`, 'error');
    } finally {
      setSaving(false);
    }
  };

  const handleCheckIn = async () => {
    if (editIndex === null) return;
    setCheckInLoading(true);
    setCheckInResult(null);
    try {
      const res = await anyRouterApi.checkIn(editIndex);
      if (res.status === 'ok') {
        const msg = res.balance !== undefined
          ? `${t('ai_providers.anyrouter_checkin_success')} (${t('ai_providers.anyrouter_balance_label')}: ${res.balance})`
          : t('ai_providers.anyrouter_checkin_success');
        setCheckInResult(msg);
        if (res.balance !== undefined) setBalance(res.balance);
        showNotification(msg, 'success');
      } else {
        const msg = `${t('ai_providers.anyrouter_checkin_failed')}: ${res.error || res.message || ''}`;
        setCheckInResult(msg);
        showNotification(msg, 'error');
      }
    } catch (err: unknown) {
      const message = getErrorMessage(err);
      const msg = `${t('ai_providers.anyrouter_checkin_failed')}: ${message}`;
      setCheckInResult(msg);
      showNotification(msg, 'error');
    } finally {
      setCheckInLoading(false);
    }
  };

  const handleRefreshBalance = async () => {
    if (editIndex === null) return;
    setBalanceLoading(true);
    try {
      const res = await anyRouterApi.getBalance(editIndex);
      if (res.balance !== undefined) {
        setBalance(res.balance);
        showNotification(t('ai_providers.anyrouter_balance_refreshed'), 'success');
      }
    } catch (err: unknown) {
      const message = getErrorMessage(err);
      showNotification(`${t('ai_providers.anyrouter_balance_error')}: ${message}`, 'error');
    } finally {
      setBalanceLoading(false);
    }
  };

  return (
    <SecondaryScreenShell
      ref={swipeRef}
      contentClassName={layoutStyles.content}
      title={title}
      onBack={handleBack}
      backLabel={t('common.back')}
      backAriaLabel={t('common.back')}
      hideTopBarBackButton
      hideTopBarRightAction
      floatingAction={
        <div className={layoutStyles.floatingActions}>
          <Button
            variant="secondary"
            size="sm"
            onClick={handleBack}
            className={layoutStyles.floatingBackButton}
          >
            {t('common.back')}
          </Button>
          <Button
            size="sm"
            onClick={() => void handleSave()}
            loading={saving}
            disabled={!canSave}
            className={layoutStyles.floatingSaveButton}
          >
            {t('common.save')}
          </Button>
        </div>
      }
      isLoading={loading}
      loadingLabel={t('common.loading')}
    >
      <Card>
        {invalidIndexParam || invalidIndex ? (
          <div className={styles.sectionHint}>{t('common.invalid_provider_index')}</div>
        ) : (
          <div className={styles.openaiEditForm}>
            <Input
              label={t('ai_providers.anyrouter_key_label')}
              value={form.apiKey}
              onChange={(e) => setForm((prev) => ({ ...prev, apiKey: e.target.value }))}
              disabled={saving || disableControls}
            />
            <Input
              label={t('ai_providers.priority_label')}
              hint={t('ai_providers.priority_hint')}
              type="number"
              step={1}
              value={form.priority ?? ''}
              onChange={(e) => {
                const raw = e.target.value;
                const parsed = raw.trim() === '' ? undefined : Number(raw);
                setForm((prev) => ({
                  ...prev,
                  priority: parsed !== undefined && Number.isFinite(parsed) ? parsed : undefined,
                }));
              }}
              disabled={saving || disableControls}
            />
            <Input
              label={t('ai_providers.anyrouter_proxy_label')}
              value={form.proxyUrl}
              onChange={(e) => setForm((prev) => ({ ...prev, proxyUrl: e.target.value }))}
              disabled={saving || disableControls}
            />

            {/* Balance display (edit mode) */}
            {editIndex !== null && (
              <div className={styles.modelConfigSection}>
                <div className={styles.modelConfigHeader}>
                  <label className={styles.modelConfigTitle}>
                    {t('ai_providers.anyrouter_balance_title')}
                  </label>
                  <div className={styles.modelConfigToolbar}>
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => void handleRefreshBalance()}
                      loading={balanceLoading}
                      disabled={saving || disableControls}
                    >
                      {t('common.refresh')}
                    </Button>
                  </div>
                </div>
                <div className={styles.sectionHint}>
                  {balance !== null
                    ? `${t('ai_providers.anyrouter_balance_label')}: ${balance}`
                    : balanceLoading
                      ? t('common.loading')
                      : t('ai_providers.anyrouter_balance_unavailable')}
                </div>
              </div>
            )}

            {/* Check-in configuration */}
            <div className={styles.modelConfigSection}>
              <div className={styles.modelConfigHeader}>
                <label className={styles.modelConfigTitle}>
                  {t('ai_providers.anyrouter_checkin_title')}
                </label>
                <div className={styles.modelConfigToolbar}>
                  <ToggleSwitch
                    checked={form.checkInEnabled}
                    onChange={(value) => setForm((prev) => ({ ...prev, checkInEnabled: value }))}
                    disabled={saving || disableControls}
                    ariaLabel={t('ai_providers.anyrouter_checkin_toggle_aria')}
                    label={t('ai_providers.anyrouter_checkin_enabled_label')}
                  />
                </div>
              </div>
              <div className={styles.sectionHint}>
                {t('ai_providers.anyrouter_checkin_hint')}
              </div>

              {form.checkInEnabled && (
                <>
                  <Input
                    label={t('ai_providers.anyrouter_checkin_userid_label')}
                    hint={t('ai_providers.anyrouter_checkin_userid_hint')}
                    value={form.checkInUserId}
                    onChange={(e) => setForm((prev) => ({ ...prev, checkInUserId: e.target.value }))}
                    disabled={saving || disableControls}
                  />
                  <Input
                    label={t('ai_providers.anyrouter_checkin_sessionid_label')}
                    value={form.checkInSessionId}
                    onChange={(e) => setForm((prev) => ({ ...prev, checkInSessionId: e.target.value }))}
                    disabled={saving || disableControls}
                  />
                  <Input
                    label={t('ai_providers.anyrouter_checkin_webhook_label')}
                    hint={t('ai_providers.anyrouter_checkin_webhook_hint')}
                    value={form.checkInWebhookUrl}
                    onChange={(e) => setForm((prev) => ({ ...prev, checkInWebhookUrl: e.target.value }))}
                    disabled={saving || disableControls}
                  />
                  {editIndex !== null && (
                    <div className={styles.modelTestPanel}>
                      <div className={styles.modelTestMeta}>
                        <label className={styles.modelTestLabel}>
                          {t('ai_providers.anyrouter_checkin_trigger_label')}
                        </label>
                        <span className={styles.modelTestHint}>
                          {t('ai_providers.anyrouter_checkin_trigger_hint')}
                        </span>
                      </div>
                      <div className={styles.modelTestControls}>
                        <Button
                          variant="secondary"
                          size="sm"
                          onClick={() => void handleCheckIn()}
                          loading={checkInLoading}
                          disabled={saving || disableControls}
                        >
                          {t('ai_providers.anyrouter_checkin_trigger_button')}
                        </Button>
                      </div>
                    </div>
                  )}
                  {checkInResult && (
                    <div className={`status-badge ${checkInResult.includes(t('ai_providers.anyrouter_checkin_success')) ? 'success' : 'error'}`}>
                      {checkInResult}
                    </div>
                  )}
                </>
              )}
            </div>

            {/* Model list (read-only) */}
            <div className={styles.modelConfigSection}>
              <div className={styles.modelConfigHeader}>
                <label className={styles.modelConfigTitle}>
                  {t('ai_providers.anyrouter_models_label')}
                </label>
              </div>
              <div className={styles.sectionHint}>
                {t('ai_providers.anyrouter_models_hint')}
              </div>
              <div className={styles.modelTagList}>
                {ANYROUTER_MODELS.map((model) => (
                  <span key={model} className={styles.modelTag}>
                    <span className={styles.modelName}>{model}</span>
                  </span>
                ))}
              </div>
            </div>
          </div>
        )}
      </Card>
    </SecondaryScreenShell>
  );
}
