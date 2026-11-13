import { useEffect, useRef, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Check,
  LoaderCircle,
  MoreHorizontal,
  Plus,
  Power,
  Route,
  RefreshCw,
  SlidersHorizontal,
  Trash2,
  Workflow,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  accountErrorKey,
  accountOptions,
  checkAccount,
  setAccountEnabled,
  type Account,
  providerLabels,
} from '@/lib/accounts'
import { CatalogDialog } from '@/components/CatalogDialog'
import { AccountUsage } from '@/components/AccountUsage'
import { AccountCredentialStatus } from '@/components/AccountCredentialStatus'
import { AccountResets } from '@/components/AccountResets'
import { ConnectAccount } from '@/components/ConnectAccount'
import { DeleteAccount } from '@/components/DeleteAccount'
import { Status } from '@/components/Status'
import { AccountLimits } from '@/components/AccountLimits'
import { AccountProxy } from '@/components/AccountProxy'
import { reasonKeys, runtimeOptions } from '@/lib/runtime'
import { proxyOptions } from '@/lib/proxies'
import { ProviderLogo } from '@/components/ProviderLogo'
import { Button } from '@/components/ui/Button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/DropdownMenu'

import { useTimeZone } from '@/lib/timezone'
export function Accounts() {
  const { t, i18n } = useTranslation()
  const timeZone = useTimeZone()
  const client = useQueryClient()
  const query = useQuery(accountOptions)
  const proxies = useQuery({
    ...proxyOptions,
    enabled:
      query.data?.accounts.some((account) => account.proxy_id !== '') ?? false,
  })
  const runtime = useQuery(runtimeOptions)
  const [clock, setClock] = useState(() => Date.now())
  const [limits, setLimits] = useState<Account | null>(null)
  const [proxyAccount, setProxyAccount] = useState<Account | null>(null)
  const [connecting, setConnecting] = useState<Account | 'new' | null>(null)
  const [removing, setRemoving] = useState<Account | null>(null)
  const [verified, setVerified] = useState<{
    name: string
    count: number
  } | null>(null)
  const cooling =
    runtime.data?.accounts.some((state) => state.state === 'cooling') ?? false
  useEffect(() => {
    const timer = window.setInterval(
      () => setClock(Date.now()),
      cooling ? 1000 : 30_000,
    )
    return () => window.clearInterval(timer)
  }, [cooling])
  const runtimeNow = runtime.data
    ? runtime.data.server_time * 1000 +
      Math.max(0, clock - runtime.dataUpdatedAt)
    : clock
  const heading = useRef<HTMLHeadingElement>(null)
  const invalidate = async () => {
    await Promise.all([
      client.invalidateQueries({ queryKey: ['accounts'] }),
      client.invalidateQueries({ queryKey: ['proxies'] }),
      client.invalidateQueries({ queryKey: ['groups'] }),
      client.invalidateQueries({ queryKey: ['model-catalog'] }),
      client.invalidateQueries({ queryKey: ['account-runtime'] }),
      client.invalidateQueries({ queryKey: ['system'] }),
      client.invalidateQueries({ queryKey: ['connection'] }),
    ])
  }
  const update = useMutation({
    mutationFn: setAccountEnabled,
    onSuccess: invalidate,
  })
  const check = useMutation({
    mutationFn: checkAccount,
    onSuccess: async (value) => {
      setVerified({ name: value.account.name, count: value.models.length })
      await invalidate()
    },
    onError: () => client.invalidateQueries({ queryKey: ['accounts'] }),
  })
  const pending = update.isPending || check.isPending
  const dates = new Intl.DateTimeFormat(i18n.resolvedLanguage ?? 'en', {
    timeZone,
    dateStyle: 'medium',
    timeStyle: 'short',
  })
  const restoreFocus = () => heading.current?.focus()
  const clear = () => {
    setVerified(null)
    update.reset()
    check.reset()
  }
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 ref={heading} tabIndex={-1} className="page-title outline-none">
            {t('accountsTitle')}
          </h1>
        </div>
        <Button
          data-tour="account"
          onClick={() => {
            clear()
            setConnecting('new')
          }}
        >
          <Plus aria-hidden="true" />
          {t('addAccount')}
        </Button>
      </div>
      {runtime.isError && (
        <p role="status" className="text-xs text-muted-foreground">
          {t('runtimeLoadFailed')}
        </p>
      )}
      {verified && (
        <p role="status" className="text-sm text-success">
          {t('accountVerificationResult', verified)}
        </p>
      )}
      {(update.error || check.error) && (
        <p role="alert" className="text-sm text-error">
          {t(accountErrorKey(update.error ?? check.error))}
        </p>
      )}
      {query.isPending ? (
        <p
          role="status"
          className="flex items-center gap-2 py-10 text-sm text-muted-foreground"
        >
          <LoaderCircle
            className="size-4 motion-safe:animate-spin"
            aria-hidden="true"
          />
          {t('loadingAccounts')}
        </p>
      ) : query.isError ? (
        <div
          role="alert"
          className="space-y-4 rounded-xl border border-border p-6"
        >
          <p className="text-sm text-error">{t('accountsLoadFailed')}</p>
          <Button variant="outline" onClick={() => query.refetch()}>
            {t('reconnect')}
          </Button>
        </div>
      ) : query.data.accounts.length === 0 ? (
        <section className="flex flex-col items-center rounded-xl border border-dashed border-border px-6 py-14 text-center">
          <Workflow
            className="mb-4 size-8 text-muted-foreground"
            aria-hidden="true"
          />
          <h2 className="font-medium">{t('accountEmptyTitle')}</h2>
          <p className="mt-2 max-w-md text-sm leading-6 text-muted-foreground">
            {t('accountEmptyDescription')}
          </p>
        </section>
      ) : (
        <ul className="space-y-3" aria-label={t('accountsTitle')}>
          {query.data.accounts.map((account) => (
            <li key={account.id}>
              <article
                aria-labelledby={`account-${account.id}`}
                className="@container overflow-hidden rounded-xl border border-border bg-card"
              >
                <div className="flex flex-wrap items-start justify-between gap-3 px-5 py-4">
                  <div className="flex min-w-0 items-start gap-3">
                    <ProviderLogo
                      provider={account.provider}
                      alt={providerLabels[account.provider]}
                      className="mt-0.5 size-6 shrink-0"
                    />
                    <div className="min-w-0 space-y-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <h2
                          id={`account-${account.id}`}
                          className="break-words text-sm font-semibold"
                        >
                          {account.name}
                        </h2>
                        {account.plan && (
                          <span className="rounded border border-border px-1.5 py-0.5 text-xs text-muted-foreground">
                            {account.plan}
                          </span>
                        )}
                      </div>
                      {account.email && (
                        <p className="break-all text-xs text-muted-foreground">
                          {account.email}
                        </p>
                      )}
                      {account.proxy_id && (
                        <p className="break-words text-xs text-muted-foreground">
                          {t('accountProxy')}:{' '}
                          {proxies.data?.proxies.find(
                            (proxy) => proxy.id === account.proxy_id,
                          )?.name ?? t('proxyAssigned')}
                        </p>
                      )}
                      <AccountCredentialStatus
                        account={account}
                        now={
                          (account.credential_refresh?.server_time ??
                            query.dataUpdatedAt / 1000) +
                          Math.max(0, clock - query.dataUpdatedAt) / 1000
                        }
                      />
                    </div>
                  </div>
                  <div className="flex items-center gap-1">
                    <Button
                      data-tour="verify"
                      variant="ghost"
                      size="icon"
                      className="size-8 text-muted-foreground [@media(pointer:coarse)]:size-11"
                      title={t('checkAccount')}
                      aria-label={t('verifyAccountConnection', {
                        name: account.name,
                      })}
                      disabled={pending || !account.enabled}
                      onClick={() => {
                        clear()
                        check.mutate(account.id)
                      }}
                    >
                      {check.isPending && check.variables === account.id ? (
                        <LoaderCircle
                          className="size-4 motion-safe:animate-spin"
                          aria-hidden="true"
                        />
                      ) : (
                        <Check className="size-4" aria-hidden="true" />
                      )}
                    </Button>
                    <DropdownMenu modal={false}>
                      <DropdownMenuTrigger asChild>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-8 text-muted-foreground [@media(pointer:coarse)]:size-11"
                          title={t('accountActions', { name: account.name })}
                          aria-label={t('accountActions', {
                            name: account.name,
                          })}
                          disabled={pending}
                        >
                          <MoreHorizontal
                            className="size-4"
                            aria-hidden="true"
                          />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem
                          onSelect={() => setProxyAccount(account)}
                        >
                          <Route aria-hidden="true" />
                          {t('accountProxy')}
                        </DropdownMenuItem>
                        <DropdownMenuItem onSelect={() => setLimits(account)}>
                          <SlidersHorizontal aria-hidden="true" />
                          {t('accountScheduling')}
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          className="[@media(pointer:coarse)]:min-h-11"
                          onSelect={() => {
                            clear()
                            setConnecting(account)
                          }}
                        >
                          <RefreshCw aria-hidden="true" />
                          {t('reauthorizeAccount')}
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          className="[@media(pointer:coarse)]:min-h-11"
                          onSelect={() => {
                            clear()
                            update.mutate({
                              id: account.id,
                              enabled: !account.enabled,
                            })
                          }}
                        >
                          <Power aria-hidden="true" />
                          {t(
                            account.enabled
                              ? 'disableAccount'
                              : 'enableAccount',
                          )}
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem
                          className="text-error focus:bg-error-muted focus:text-error [@media(pointer:coarse)]:min-h-11"
                          onSelect={() => {
                            clear()
                            setRemoving(account)
                          }}
                        >
                          <Trash2 aria-hidden="true" />
                          {t('deleteAccount')}
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                </div>
                <div className="grid gap-5 border-t border-border px-5 py-4 @2xl:grid-cols-[minmax(0,1fr)_minmax(14rem,0.65fr)]">
                  <div className="min-w-0 space-y-3">
                    {account.enabled && account.status !== 'reauth_required' ? (
                      <AccountUsage
                        id={account.id}
                        name={account.name}
                        provider={account.provider}
                      />
                    ) : (
                      <div className="space-y-2">
                        <h3 className="text-xs font-medium text-muted-foreground">
                          {t('accountUsage')}
                        </h3>
                        <p className="text-sm leading-5 text-muted-foreground">
                          {t(
                            account.enabled
                              ? 'accountReauthorizeHint'
                              : 'accountDisabledHint',
                          )}
                        </p>
                      </div>
                    )}
                    {account.provider === 'codex' && (
                      <AccountResets
                        id={account.id}
                        name={account.name}
                        enabled={
                          account.enabled &&
                          account.status !== 'reauth_required'
                        }
                      />
                    )}
                  </div>
                  <div className="min-w-0 space-y-4 border-t border-border pt-4 @2xl:border-l @2xl:border-t-0 @2xl:pl-5 @2xl:pt-0">
                    {runtime.data?.accounts
                      .filter((state) => state.id === account.id)
                      .map((state) => (
                        <div
                          key={state.id}
                          className="space-y-2 text-sm text-muted-foreground"
                        >
                          <p className="tabular-nums">
                            {t('accountInFlight', {
                              active: state.in_flight,
                              limit: state.max_concurrency,
                            })}
                          </p>
                          {account.enabled &&
                            account.status !== 'reauth_required' &&
                            state.limited_models > 0 && (
                              <p className="text-xs text-warning">
                                {t('accountModelLimits', {
                                  count: state.limited_models,
                                })}
                              </p>
                            )}
                          {account.enabled &&
                            account.status !== 'reauth_required' &&
                            state.state !== 'available' && (
                              <>
                                <Status kind="warning">
                                  {t(
                                    state.state === 'quota_exhausted'
                                      ? 'accountQuotaExhausted'
                                      : state.state === 'cooling'
                                        ? 'accountCooling'
                                        : state.state === 'probing'
                                          ? 'accountProbing'
                                          : 'accountRetryReady',
                                  )}
                                </Status>
                                {state.state === 'quota_exhausted' && (
                                  <p className="text-xs leading-5">
                                    {t('accountQuotaRoutingHint')}
                                  </p>
                                )}
                                {state.state === 'cooling' && (
                                  <div className="space-y-1 text-xs leading-5">
                                    <p>
                                      {t('accountCoolingReason', {
                                        reason: t(
                                          reasonKeys[state.reason] ??
                                            'reasonUnknown',
                                        ),
                                      })}
                                    </p>
                                    <p>
                                      {t('accountRetryAt', {
                                        time: dates.format(
                                          state.cooldown_until * 1000,
                                        ),
                                        seconds: Math.max(
                                          0,
                                          state.cooldown_until -
                                            Math.floor(runtimeNow / 1000),
                                        ),
                                      })}
                                    </p>
                                    <p>{t('accountCoolingHint')}</p>
                                  </div>
                                )}
                              </>
                            )}
                        </div>
                      ))}
                    {account.group_count === 0 && (
                      <div className="flex flex-wrap items-center gap-2">
                        <Status kind="neutral">{t('accountUnassigned')}</Status>
                        <Link
                          to="/groups"
                          className="text-xs underline underline-offset-4"
                        >
                          {t('accountAssignGroup')}
                        </Link>
                      </div>
                    )}
                    <div className="space-y-1">
                      <p className="text-xs text-muted-foreground">
                        {t('accountExpires')}
                      </p>
                      <p className="text-sm tabular-nums">
                        {account.expires_at ? (
                          <time
                            dateTime={new Date(
                              account.expires_at * 1000,
                            ).toISOString()}
                          >
                            {dates.format(account.expires_at * 1000)}
                          </time>
                        ) : (
                          t('accountUnknownExpiry')
                        )}
                      </p>
                    </div>
                    <div className="-ml-2">
                      <CatalogDialog
                        target={{ kind: 'account', id: account.id }}
                        name={account.name}
                        disabled={
                          !account.enabled ||
                          account.status === 'reauth_required'
                        }
                      />
                    </div>
                  </div>
                </div>
              </article>
            </li>
          ))}
        </ul>
      )}
      {limits && (
        <AccountLimits
          account={limits}
          runtime={runtime.data?.accounts.find(
            (state) => state.id === limits.id,
          )}
          onClose={() => setLimits(null)}
          onChanged={invalidate}
          restoreFocus={restoreFocus}
        />
      )}
      {proxyAccount && (
        <AccountProxy
          account={proxyAccount}
          onClose={() => setProxyAccount(null)}
          onChanged={invalidate}
          restoreFocus={restoreFocus}
        />
      )}
      {connecting && (
        <ConnectAccount
          account={connecting === 'new' ? undefined : connecting}
          onClose={() => setConnecting(null)}
          onCreated={invalidate}
          restoreFocus={restoreFocus}
        />
      )}
      {removing && (
        <DeleteAccount
          account={removing}
          onClose={() => setRemoving(null)}
          onDeleted={invalidate}
          restoreFocus={restoreFocus}
        />
      )}
    </div>
  )
}
