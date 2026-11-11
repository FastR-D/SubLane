import { useRef, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Check,
  LoaderCircle,
  MoreHorizontal,
  Pencil,
  Plus,
  Power,
  Route,
  SlidersHorizontal,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  channelOptions,
  reportsOpenCodeUsage,
  channelRuntimeOptions,
  channelErrorKey,
  channelLoadErrorKey,
  checkChannel,
  setChannelEnabled,
  deleteChannel,
  bindChannelProxy,
  setChannelConcurrency,
  resumeChannel,
  type Channel,
  channelPlatforms,
  channelProtocols,
} from '@/lib/channels'
import { reasonKeys } from '@/lib/runtime'
import { ChannelLogo } from '@/components/ChannelLogo'
import { ChannelEditor } from '@/components/ChannelEditor'
import { AccountUsage } from '@/components/AccountUsage'
import { CatalogDialog } from '@/components/CatalogDialog'
import { AccountLimits } from '@/components/AccountLimits'
import { AccountProxy } from '@/components/AccountProxy'
import { DeleteAccount } from '@/components/DeleteAccount'
import { Status } from '@/components/Status'
import { Button } from '@/components/ui/Button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/DropdownMenu'

export function Channels() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const query = useQuery(channelOptions)
  const runtime = useQuery(channelRuntimeOptions)
  const heading = useRef<HTMLHeadingElement>(null)
  const [editing, setEditing] = useState<Channel | null>(null)
  const [removing, setRemoving] = useState<Channel | null>(null)
  const [limits, setLimits] = useState<Channel | null>(null)
  const [proxy, setProxy] = useState<Channel | null>(null)
  const [verified, setVerified] = useState<{
    name: string
    count: number
  } | null>(null)
  const restoreFocus = () => heading.current?.focus()
  const invalidate = async () => {
    await Promise.all(
      [
        'channels',
        'channel-runtime',
        'groups',
        'model-catalog',
        'available-groups',
        'connection',
        'system',
        'keys',
      ].map((key) => client.invalidateQueries({ queryKey: [key] })),
    )
  }
  const update = useMutation({
    mutationFn: setChannelEnabled,
    onSuccess: invalidate,
  })
  const check = useMutation({
    mutationFn: checkChannel,
    onSuccess: async (value) => {
      setVerified({ name: value.channel.name, count: value.models.length })
      await invalidate()
    },
    onError: () => client.invalidateQueries({ queryKey: ['channels'] }),
  })
  const busy = update.isPending || check.isPending
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <h1 ref={heading} tabIndex={-1} className="page-title">
          {t('channelsTitle')}
        </h1>
        <Button asChild data-tour="account">
          <Link to="/channels/new">
            <Plus aria-hidden="true" />
            {t('addChannel')}
          </Link>
        </Button>
      </div>
      <p className="max-w-3xl text-sm leading-6 text-muted-foreground">
        {t('channelsDescription')}
      </p>
      {verified && (
        <p role="status" className="text-sm text-success">
          {t('channelVerifiedModels', verified)}
        </p>
      )}
      {(check.isError || update.isError) && (
        <p role="alert" className="text-sm text-error">
          {t(channelErrorKey(check.error ?? update.error))}
        </p>
      )}
      {query.isPending ? (
        <p
          role="status"
          className="flex items-center gap-2 py-10 text-sm text-muted-foreground"
        >
          <LoaderCircle
            aria-hidden="true"
            className="size-4 motion-safe:animate-spin"
          />
          {t('loadingChannels')}
        </p>
      ) : query.isError ? (
        <div
          role="alert"
          className="space-y-3 rounded-xl border border-border p-6"
        >
          <p className="text-sm text-error">
            {t(channelLoadErrorKey(query.error))}
          </p>
          <Button
            data-tour="verify"
            variant="outline"
            onClick={() => query.refetch()}
            disabled={query.isFetching}
          >
            {t('reconnect')}
          </Button>
        </div>
      ) : query.data.channels.length === 0 ? (
        <div className="space-y-2 rounded-xl border border-border p-6">
          <h2 className="font-medium">{t('channelsEmpty')}</h2>
          <p className="text-sm leading-6 text-muted-foreground">
            {t('channelsEmptyHint')}
          </p>
        </div>
      ) : (
        <ul className="space-y-3" aria-label={t('channelsTitle')}>
          {query.data.channels.map((channel) => {
            const state = runtime.data?.channels.find(
              (value) => value.id === channel.id,
            )
            return (
              <li key={channel.id}>
                <article
                  aria-labelledby={`channel-${channel.id}`}
                  className="overflow-hidden rounded-xl border border-border bg-card"
                >
                  <div className="flex flex-wrap items-start justify-between gap-3 px-5 py-4">
                    <div className="flex min-w-0 items-start gap-3">
                      <ChannelLogo
                        platform={channel.platform}
                        className="mt-0.5 size-7"
                      />
                      <div className="min-w-0 space-y-1">
                        <h2
                          id={`channel-${channel.id}`}
                          className="break-words text-sm font-semibold"
                        >
                          {channel.name}
                        </h2>
                        <p className="break-all text-xs text-muted-foreground">
                          {channel.base_url}
                        </p>
                        <p className="text-xs text-muted-foreground">
                          {t(channelPlatforms[channel.platform].label)} ·{' '}
                          {t(channelProtocols[channel.protocol])}
                        </p>
                      </div>
                    </div>
                    <div className="flex items-center gap-1">
                      <Status
                        kind={
                          !channel.enabled
                            ? 'neutral'
                            : channel.status === 'ready'
                              ? 'success'
                              : channel.status === 'key_required'
                                ? 'error'
                                : 'warning'
                        }
                      >
                        {t(
                          !channel.enabled
                            ? 'disabled'
                            : channel.status === 'ready'
                              ? 'accountVerified'
                              : channel.status === 'key_required'
                                ? 'channelNeedsKey'
                                : 'accountUnverified',
                        )}
                      </Status>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-8 [@media(pointer:coarse)]:size-11"
                        aria-label={t('verifyChannelConnection', {
                          name: channel.name,
                        })}
                        title={t('checkAccount')}
                        disabled={busy || !channel.enabled}
                        onClick={() => {
                          setVerified(null)
                          check.mutate(channel.id)
                        }}
                      >
                        {check.isPending && check.variables === channel.id ? (
                          <LoaderCircle
                            aria-hidden="true"
                            className="size-4 motion-safe:animate-spin"
                          />
                        ) : (
                          <Check aria-hidden="true" className="size-4" />
                        )}
                      </Button>
                      <DropdownMenu modal={false}>
                        <DropdownMenuTrigger asChild>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="size-8 [@media(pointer:coarse)]:size-11"
                            aria-label={t('channelActions', {
                              name: channel.name,
                            })}
                            disabled={busy}
                          >
                            <MoreHorizontal
                              aria-hidden="true"
                              className="size-4"
                            />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem
                            onSelect={() => setEditing(channel)}
                          >
                            <Pencil aria-hidden="true" />
                            {t('editChannel')}
                          </DropdownMenuItem>
                          <DropdownMenuItem onSelect={() => setProxy(channel)}>
                            <Route aria-hidden="true" />
                            {t('accountProxy')}
                          </DropdownMenuItem>
                          <DropdownMenuItem onSelect={() => setLimits(channel)}>
                            <SlidersHorizontal aria-hidden="true" />
                            {t('accountScheduling')}
                          </DropdownMenuItem>
                          <DropdownMenuItem
                            onSelect={() =>
                              update.mutate({
                                id: channel.id,
                                enabled: !channel.enabled,
                              })
                            }
                          >
                            <Power aria-hidden="true" />
                            {t(
                              channel.enabled
                                ? 'disableChannel'
                                : 'enableChannel',
                            )}
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem
                            className="text-error focus:bg-error-muted focus:text-error"
                            onSelect={() => setRemoving(channel)}
                          >
                            <Trash2 aria-hidden="true" />
                            {t('deleteChannel')}
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </div>
                  </div>
                  <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border px-5 py-4">
                    <div className="min-w-0 space-y-2 text-sm text-muted-foreground">
                      {state ? (
                        <p>
                          {t('accountInFlight', {
                            active: state.in_flight,
                            limit: state.max_concurrency,
                          })}
                        </p>
                      ) : (
                        <p>{t('channelRuntimeUnknown')}</p>
                      )}
                      {runtime.isError && (
                        <p role="alert" className="text-error">
                          {t('channelRuntimeFailed')}
                        </p>
                      )}
                      {state && state.state !== 'available' && (
                        <p>{t(reasonKeys[state.reason] ?? 'reasonUnknown')}</p>
                      )}
                      <div className="flex flex-wrap items-center gap-2">
                        {channel.group_count !== undefined && (
                          <Status kind="neutral">
                            {t(
                              channel.group_count
                                ? 'resourceAssigned'
                                : 'resourceUnassigned',
                              { count: channel.group_count },
                            )}
                          </Status>
                        )}
                        <Link
                          to="/groups"
                          className="underline underline-offset-4"
                        >
                          {t('channelAssignGroup')}
                        </Link>
                      </div>
                    </div>
                    <CatalogDialog
                      target={{ kind: 'channel', id: channel.id }}
                      name={channel.name}
                      disabled={
                        !channel.enabled || channel.status === 'key_required'
                      }
                    />
                  </div>
                  {reportsOpenCodeUsage(channel) &&
                    channel.enabled &&
                    channel.status !== 'key_required' && (
                      <div className="border-t border-border px-5 py-4">
                        <AccountUsage
                          id={channel.id}
                          name={channel.name}
                          provider="openai"
                          resource="channels"
                        />
                      </div>
                    )}
                  {channel.status === 'key_required' && (
                    <p className="border-t border-border px-5 py-3 text-sm text-error">
                      {t('apiKeyReplacementHint')}
                    </p>
                  )}
                </article>
              </li>
            )
          })}
        </ul>
      )}
      <p className="text-xs leading-5 text-muted-foreground">
        {t('apiUpstreamUsageHint')}
      </p>
      {editing && (
        <ChannelEditor
          channel={editing}
          onClose={() => setEditing(null)}
          onSaved={invalidate}
          restoreFocus={restoreFocus}
        />
      )}
      {removing && (
        <DeleteAccount
          account={removing}
          remove={deleteChannel}
          titleKey="deleteChannelTitle"
          descriptionKey="deleteChannelDescription"
          actionKey="deleteChannel"
          onClose={() => setRemoving(null)}
          onDeleted={invalidate}
          restoreFocus={restoreFocus}
        />
      )}
      {proxy && (
        <AccountProxy
          account={proxy}
          bindProxy={bindChannelProxy}
          hint="apiUpstreamProxyHint"
          onClose={() => setProxy(null)}
          onChanged={invalidate}
          restoreFocus={restoreFocus}
        />
      )}
      {limits && (
        <AccountLimits
          account={limits}
          runtime={runtime.data?.channels.find(
            (value) => value.id === limits.id,
          )}
          updateConcurrency={setChannelConcurrency}
          resumeResource={resumeChannel}
          onClose={() => setLimits(null)}
          onChanged={invalidate}
          restoreFocus={restoreFocus}
        />
      )}
    </div>
  )
}
