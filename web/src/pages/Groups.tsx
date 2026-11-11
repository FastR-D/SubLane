import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { FolderClosed, LoaderCircle, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  deleteGroup,
  groupDeleteErrorKey,
  groupOptions,
  type Group,
} from '@/lib/groups'
import { CatalogDialog } from '@/components/CatalogDialog'
import { ConfirmDelete } from '@/components/ConfirmDelete'
import { GroupEditor } from '@/components/GroupEditor'
import { Button } from '@/components/ui/Button'
import { Status } from '@/components/Status'

export function Groups() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const query = useQuery(groupOptions)
  const [editing, setEditing] = useState<number | null>(null)
  const [removing, setRemoving] = useState<Group | null>(null)
  const saved = async () => {
    await Promise.all([
      client.invalidateQueries({ queryKey: ['groups'] }),
      client.invalidateQueries({ queryKey: ['accounts'] }),
      client.invalidateQueries({ queryKey: ['channels'] }),
      client.invalidateQueries({ queryKey: ['model-catalog'] }),
      client.invalidateQueries({ queryKey: ['available-groups'] }),
      client.invalidateQueries({ queryKey: ['keys'] }),
      client.invalidateQueries({ queryKey: ['connection'] }),
    ])
    setEditing(null)
  }
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="page-title">{t('accountGroups')}</h1>
        </div>
        <Button data-tour="pool" onClick={() => setEditing(0)}>
          <Plus aria-hidden="true" />
          {t('createGroup')}
        </Button>
      </div>
      <p className="max-w-3xl text-sm leading-6 text-muted-foreground">
        {t('groupsAssignmentHint')}
      </p>
      {query.isPending ? (
        <p
          role="status"
          className="flex items-center gap-2 py-10 text-sm text-muted-foreground"
        >
          <LoaderCircle
            aria-hidden="true"
            className="size-4 motion-safe:animate-spin"
          />
          {t('loadingGroups')}
        </p>
      ) : query.isError ? (
        <div
          role="alert"
          className="space-y-4 rounded-xl border border-border p-6"
        >
          <p className="text-sm text-error">{t('groupsLoadFailed')}</p>
          <Button
            variant="outline"
            onClick={() => query.refetch()}
            disabled={query.isFetching}
          >
            {t('reconnect')}
          </Button>
        </div>
      ) : query.data.groups.length === 0 ? (
        <p className="rounded-xl border border-border p-6 text-sm text-muted-foreground">
          {t('groupsEmpty')}
        </p>
      ) : (
        <div className="divide-y divide-border rounded-xl border border-border bg-card">
          {query.data.groups.map((group) => (
            <section
              key={group.id}
              aria-label={group.name}
              className="flex flex-wrap items-center justify-between gap-4 p-5"
            >
              <div className="min-w-0 space-y-2">
                <div className="flex flex-wrap items-center gap-3">
                  <FolderClosed
                    className="size-4 shrink-0 text-muted-foreground"
                    aria-hidden="true"
                  />
                  <h2 className="break-words font-medium">{group.name}</h2>
                  <Status kind={group.enabled ? 'success' : 'neutral'}>
                    {t(group.enabled ? 'active' : 'disabled')}
                  </Status>
                </div>
                <p className="text-sm text-muted-foreground">
                  {t(
                    group.restricted_models
                      ? 'groupRestrictedModels'
                      : 'groupAllModels',
                  )}{' '}
                  ·{' '}
                  {t('groupCounts', {
                    accounts: group.account_count,
                    members: group.member_count,
                  })}
                </p>
                <p className="text-xs text-muted-foreground">
                  {group.channel_count !== undefined &&
                  group.subscription_count !== undefined &&
                  group.channel_count > 0 &&
                  group.subscription_count === 0 ? (
                    t('routingAPIOnly')
                  ) : group.subscription_count !== undefined &&
                    group.channel_count === 0 &&
                    group.subscription_count > 0 ? (
                    t('routingSubscriptionsOnly')
                  ) : (
                    <>
                      {t(
                        group.routing.preference === 'subscription_first'
                          ? 'routingSubscriptionFirst'
                          : group.routing.preference === 'api_first'
                            ? 'routingAPIFirst'
                            : 'routingProtocol',
                      )}
                      {group.routing.preference === 'subscription_first' &&
                        ` · ${t(group.routing.allow_api_fallback ? 'apiFallbackEnabled' : 'apiFallbackDisabled')}`}
                    </>
                  )}
                  {group.routing.session_sticky &&
                    ` · ${t('sessionStickyEnabled')}`}
                </p>
              </div>
              <div className="flex items-center gap-2">
                <CatalogDialog
                  target={{ kind: 'group', id: group.id }}
                  name={group.name}
                  disabled={!group.enabled}
                />
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setEditing(group.id)}
                  aria-label={t('editGroupNamed', {
                    name: group.name,
                  })}
                >
                  {t('editGroup')}
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-8 text-muted-foreground hover:text-error [@media(pointer:coarse)]:size-11"
                  onClick={() => setRemoving(group)}
                  title={t('deleteGroup')}
                  aria-label={t('deleteGroupNamed', { name: group.name })}
                >
                  <Trash2 aria-hidden="true" className="size-4" />
                </Button>
              </div>
            </section>
          ))}
        </div>
      )}
      {editing !== null && (
        <GroupEditor
          id={editing || undefined}
          onClose={() => setEditing(null)}
          onSaved={saved}
        />
      )}
      {removing && (
        <ConfirmDelete
          title={t('deleteGroupTitle')}
          description={t('deleteGroupDescription', { name: removing.name })}
          action={t('deleteGroup')}
          remove={() => deleteGroup(removing.id)}
          errorText={(error) => t(groupDeleteErrorKey(error))}
          onClose={() => setRemoving(null)}
          onDeleted={saved}
        />
      )}
    </div>
  )
}
