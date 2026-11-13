import { useTranslation } from 'react-i18next'
import type { Account } from '@/lib/accounts'
import { useTimeZone } from '@/lib/timezone'
import { Status } from './Status'

export function AccountCredentialStatus({
  account,
  now,
}: {
  account: Account
  now: number
}) {
  const { t, i18n } = useTranslation()
  const timeZone = useTimeZone()
  const codex = account.provider === 'codex'
  const reauthorization = account.status === 'reauth_required'
  const expired = codex && account.expires_at > 0 && account.expires_at <= now
  const refresh = codex ? account.credential_refresh : undefined
  const dates = new Intl.DateTimeFormat(i18n.resolvedLanguage ?? 'en', {
    timeZone,
    dateStyle: 'medium',
    timeStyle: 'short',
  })
  const showCredential = codex || account.enabled
  return (
    <div className="space-y-1">
      <div className="flex flex-wrap gap-1.5">
        {!account.enabled && <Status kind="neutral">{t('disabled')}</Status>}
        {showCredential && (
          <Status
            kind={
              reauthorization
                ? 'error'
                : expired
                  ? 'warning'
                  : account.status === 'ready'
                    ? 'success'
                    : 'warning'
            }
          >
            {t(
              reauthorization
                ? 'accountNeedsAuth'
                : expired
                  ? 'accountTokenExpired'
                  : account.status === 'ready'
                    ? 'accountVerified'
                    : 'accountUnverified',
            )}
          </Status>
        )}
        {!reauthorization && refresh?.refreshing && (
          <Status kind="info">{t('accountCredentialRefreshing')}</Status>
        )}
        {!reauthorization && refresh?.failed && (
          <Status kind="warning">{t('accountCredentialRefreshFailed')}</Status>
        )}
      </div>
      {!reauthorization && refresh?.failed && (
        <div role="status" className="space-y-1 text-xs leading-5 text-warning">
          {refresh.last_failure_at > 0 && (
            <p>
              {t('accountCredentialLastFailure', {
                time: dates.format(refresh.last_failure_at * 1000),
              })}
            </p>
          )}
          {!refresh.refreshing && (
            <p>
              {refresh.retry_at > now
                ? t('accountCredentialRetryAt', {
                    time: dates.format(refresh.retry_at * 1000),
                  })
                : t('accountCredentialRetryPending')}
            </p>
          )}
        </div>
      )}
    </div>
  )
}
