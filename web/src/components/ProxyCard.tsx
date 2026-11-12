import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ExternalLink,
  LoaderCircle,
  Pencil,
  RefreshCw,
  ScanSearch,
  ShieldCheck,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/cn'
import {
  ipReportErrorKey,
  ipReportOptions,
  ipScoreTier,
  refreshIPReport,
  type IPReport,
  type Proxy,
} from '@/lib/proxies'
import { useTimeZone } from '@/lib/timezone'
import { Status } from './Status'
import { Button } from './ui/Button'

const toneText = {
  success: 'text-success',
  info: 'text-info',
  warning: 'text-warning',
  error: 'text-error',
  neutral: 'text-muted-foreground',
} as const

const riskFlags = [
  ['vpn', 'ipReportVPN'],
  ['proxy', 'ipReportProxy'],
  ['tor', 'ipReportTor'],
  ['abuser', 'ipReportAbuser'],
  ['crawler', 'ipReportCrawler'],
] as const

function networkKind(report: IPReport) {
  if (report.mobile) return 'ipReportMobile'
  if (report.residential) return 'ipReportResidential'
  if (report.datacenter || report.company_type === 'hosting')
    return 'ipReportDatacenter'
  if (report.company_type === 'business') return 'ipReportBusiness'
  if (report.company_type === 'isp') return 'ipReportISP'
  return 'ipReportUnknownType'
}

function regionName(locale: string, code: string, fallback: string) {
  if (!code) return fallback
  try {
    return (
      new Intl.DisplayNames(locale, { type: 'region' }).of(code) ?? fallback
    )
  } catch {
    return fallback
  }
}

// One card per proxy: connection facts and the exit IP report share each field exactly once.
export function ProxyCard({
  proxy,
  checking,
  checkDisabled,
  deleteDisabled,
  onCheck,
  onEdit,
  onDelete,
}: {
  proxy: Proxy
  checking: boolean
  checkDisabled: boolean
  deleteDisabled: boolean
  onCheck: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const { t, i18n } = useTranslation()
  const timeZone = useTimeZone()
  const client = useQueryClient()
  const locale = i18n.resolvedLanguage ?? 'en'
  const ready = proxy.reachable && proxy.exit_ip !== ''
  const options = ipReportOptions(proxy.id, proxy.exit_ip)
  const query = useQuery({ ...options, enabled: ready })
  const refresh = useMutation({
    mutationFn: () => refreshIPReport(proxy.id),
    onSuccess: (data) => client.setQueryData(options.queryKey, data),
  })
  const report = query.data
  const error = refresh.error ?? query.error
  const busy = query.isFetching || refresh.isPending
  const tier = ipScoreTier(report?.trust_score ?? null)
  const flags = report ? riskFlags.filter(([key]) => report[key]) : []
  // The reputation report usually resolves a fuller place; the check result remains the fallback.
  const location = (
    report
      ? [
          report.city,
          report.region,
          regionName(locale, report.country_code, report.country),
        ]
      : [
          proxy.city,
          proxy.region,
          regionName(locale, proxy.country, proxy.country),
        ]
  )
    .filter((value, index, values) => value && values.indexOf(value) === index)
    .join(', ')
  const network = report
    ? [
        report.asn > 0 ? `AS${report.asn}` : '',
        report.as_organization || report.isp,
      ]
        .filter(Boolean)
        .join(' · ')
    : ''
  const dates = new Intl.DateTimeFormat(locale, {
    timeZone,
    dateStyle: 'medium',
    timeStyle: 'short',
  })
  return (
    <article
      aria-labelledby={`proxy-${proxy.id}`}
      className="flex h-full min-w-0 flex-col overflow-hidden rounded-xl border border-border bg-card"
    >
      <div className="flex items-start justify-between gap-2 px-4 pt-4">
        <div className="min-w-0 space-y-1">
          <h2
            id={`proxy-${proxy.id}`}
            className="truncate text-sm font-semibold"
            title={proxy.name}
          >
            {proxy.name}
          </h2>
          <p
            className="truncate text-xs text-muted-foreground"
            title={proxy.endpoint}
          >
            {proxy.endpoint}
          </p>
        </div>
        <div className="-mr-1.5 -mt-1 flex shrink-0 items-center">
          <Button
            variant="ghost"
            size="icon"
            className="size-8 text-muted-foreground [@media(pointer:coarse)]:size-11"
            title={t('testProxyNamed', { name: proxy.name })}
            aria-label={t('testProxyNamed', { name: proxy.name })}
            disabled={checkDisabled}
            onClick={onCheck}
          >
            {checking ? (
              <LoaderCircle
                className="motion-safe:animate-spin"
                aria-hidden="true"
              />
            ) : (
              <ScanSearch aria-hidden="true" />
            )}
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="size-8 text-muted-foreground [@media(pointer:coarse)]:size-11"
            title={t('editProxyNamed', { name: proxy.name })}
            aria-label={t('editProxyNamed', { name: proxy.name })}
            onClick={onEdit}
          >
            <Pencil aria-hidden="true" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="size-8 text-error hover:text-error focus-visible:text-error [@media(pointer:coarse)]:size-11"
            title={t('deleteProxyNamed', { name: proxy.name })}
            aria-label={t('deleteProxyNamed', { name: proxy.name })}
            disabled={deleteDisabled}
            onClick={onDelete}
          >
            <Trash2 aria-hidden="true" />
          </Button>
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-1.5 px-4 pt-3">
        {proxy.checked_at === 0 ? (
          <Status kind="neutral">{t('proxyNotTested')}</Status>
        ) : (
          <Status
            kind={
              proxy.reachable
                ? 'success'
                : proxy.check_error === 'connection_failed'
                  ? 'error'
                  : 'warning'
            }
          >
            {t(
              proxy.reachable
                ? 'proxyConnected'
                : proxy.check_error === 'lookup_rate_limited'
                  ? 'proxyLookupRateLimited'
                  : proxy.check_error === 'connection_failed'
                    ? 'proxyConnectionFailed'
                    : 'proxyLookupUnavailable',
            )}
          </Status>
        )}
        {proxy.reachable && (
          <span className="rounded-md bg-muted px-2 py-1 text-xs tabular-nums text-muted-foreground">
            {t('proxyLatency', { latency: proxy.latency_ms })}
          </span>
        )}
      </div>
      <section
        aria-label={t('ipReportLabel', { name: proxy.name })}
        className="mx-4 mt-3 space-y-2.5 rounded-lg bg-muted/40 p-3"
      >
        {!ready ? (
          <p className="text-xs leading-5 text-muted-foreground">
            {t('ipReportNeedsCheck')}
          </p>
        ) : query.isPending || (refresh.isPending && !report) ? (
          <p role="status" className="text-xs text-muted-foreground">
            {t('ipReportLoading')}
          </p>
        ) : !report ? (
          <div className="space-y-2">
            {error && (
              <p role="alert" className="text-xs text-warning">
                {t(ipReportErrorKey(error))}
              </p>
            )}
            <p className="text-xs leading-5 text-muted-foreground">
              {t('ipReportLookupHint')}
            </p>
            <Button
              variant="outline"
              size="sm"
              disabled={refresh.isPending}
              aria-label={t('ipReportLookupNamed', { name: proxy.name })}
              onClick={() => refresh.mutate()}
            >
              <ShieldCheck aria-hidden="true" />
              {t('ipReportLookup')}
            </Button>
          </div>
        ) : (
          <>
            <div className="flex items-center justify-between gap-2">
              <div className="flex items-baseline gap-1">
                <span
                  className={cn(
                    'text-2xl font-semibold leading-none tabular-nums',
                    toneText[tier.tone],
                  )}
                >
                  {report.trust_score ?? '—'}
                </span>
                <span className="text-xs text-muted-foreground">/ 100</span>
              </div>
              <Status kind={tier.tone}>{t(tier.label)}</Status>
            </div>
            <div
              className="h-1.5 overflow-hidden rounded-full bg-muted"
              role="meter"
              aria-label={t('ipReportScore')}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={report.trust_score ?? undefined}
            >
              <div
                className={cn(
                  'h-full rounded-full bg-current',
                  toneText[tier.tone],
                )}
                style={{ width: `${report.trust_score ?? 0}%` }}
              />
            </div>
            <div className="flex flex-wrap gap-1.5">
              <Status kind="info">{t(networkKind(report))}</Status>
              {flags.length === 0 ? (
                <Status kind="success">{t('ipReportNoRisk')}</Status>
              ) : (
                flags.map(([key, label]) => (
                  <Status
                    key={key}
                    kind={
                      key === 'tor' || key === 'abuser' ? 'error' : 'warning'
                    }
                  >
                    {t(label)}
                  </Status>
                ))
              )}
            </div>
          </>
        )}
        {report && error && (
          <p role="alert" className="text-xs text-warning">
            {t(ipReportErrorKey(error))}
          </p>
        )}
      </section>
      <dl className="grid flex-1 grid-cols-[auto_minmax(0,1fr)] content-start gap-x-3 gap-y-1.5 px-4 py-3 text-xs">
        <dt className="text-muted-foreground">{t('proxyExitIPLabel')}</dt>
        <dd className="break-all font-mono">
          {proxy.reachable && proxy.exit_ip ? proxy.exit_ip : '—'}
        </dd>
        <dt className="text-muted-foreground">{t('proxyLocationLabel')}</dt>
        <dd className="break-words">
          {proxy.reachable ? location || t('proxyLocationUnknown') : '—'}
        </dd>
        {network && (
          <>
            <dt className="text-muted-foreground">{t('proxyNetworkLabel')}</dt>
            <dd className="break-words">{network}</dd>
          </>
        )}
        {report?.verdict && (
          <>
            <dt className="text-muted-foreground">
              {t('ipReportVerdictLabel')}
            </dt>
            <dd className="break-words">{report.verdict}</dd>
          </>
        )}
        <dt className="text-muted-foreground">{t('proxyBoundAccounts')}</dt>
        <dd className="tabular-nums">{proxy.account_count}</dd>
      </dl>
      <div className="flex items-center justify-between gap-2 border-t border-border px-4 py-2 text-xs text-muted-foreground">
        <span className="min-w-0 truncate">
          {proxy.checked_at === 0 ? (
            t('proxyNotTested')
          ) : (
            <time dateTime={new Date(proxy.checked_at * 1000).toISOString()}>
              {t('proxyCheckedAt', {
                time: dates.format(proxy.checked_at * 1000),
              })}
            </time>
          )}
        </span>
        {ready && report && (
          <div className="-mr-1.5 flex shrink-0 items-center">
            <Button
              asChild
              variant="ghost"
              size="icon"
              className="size-7 text-muted-foreground [@media(pointer:coarse)]:size-11"
            >
              <a
                href={`https://ip.net.coffee/ip/${encodeURIComponent(proxy.exit_ip)}`}
                target="_blank"
                rel="noopener noreferrer"
                title={t('ipReportOpen')}
                aria-label={t('ipReportOpen')}
              >
                <ExternalLink aria-hidden="true" className="size-3.5" />
              </a>
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="size-7 text-muted-foreground [@media(pointer:coarse)]:size-11"
              disabled={busy}
              title={t('ipReportRefresh')}
              aria-label={t('ipReportRefreshNamed', { name: proxy.name })}
              onClick={() => refresh.mutate()}
            >
              <RefreshCw
                aria-hidden="true"
                className={cn('size-3.5', busy && 'motion-safe:animate-spin')}
              />
            </Button>
          </div>
        )}
      </div>
    </article>
  )
}
