import { useRef, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { accountOptions, providerLabels } from '@/lib/accounts'
import { channelOptions } from '@/lib/channels'
import { Link } from '@tanstack/react-router'
import { ChevronDown } from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from './ui/DropdownMenu'
import { authKey, type AuthState } from '@/lib/auth'
import {
  groupDetails,
  groupErrorKey,
  saveGroup,
  type GroupDetail,
} from '@/lib/groups'
import { Button } from './ui/Button'
import { Input } from './ui/Input'
import { Textarea } from './ui/Textarea'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from './ui/Dialog'

type Props = { id?: number; onClose: () => void; onSaved: () => Promise<void> }
export function GroupEditor({ id, onClose, onSaved }: Props) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const previousFocus = useRef(document.activeElement as HTMLElement | null)
  const restoreFocus = () => previousFocus.current?.focus()
  const query = useQuery({
    queryKey: ['group', id],
    queryFn: ({ signal }) => groupDetails(id!, signal),
    enabled: () =>
      Boolean(id) &&
      client.getQueryData<AuthState>(authKey)?.user?.role === 'admin',
    staleTime: 0,
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      {!id || (query.data && !query.isFetching && !query.isError) ? (
        <GroupForm
          value={query.data}
          onClose={onClose}
          onSaved={onSaved}
          restoreFocus={restoreFocus}
        />
      ) : (
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('editGroup')}</DialogTitle>
            <DialogDescription>{t('groupEditorDescription')}</DialogDescription>
          </DialogHeader>
          {query.isError ? (
            <div role="alert" className="space-y-3">
              <p className="text-sm text-error">{t('groupsLoadFailed')}</p>
              <Button variant="outline" onClick={() => query.refetch()}>
                {t('reconnect')}
              </Button>
            </div>
          ) : (
            <p role="status" className="text-sm text-muted-foreground">
              {t('loadingGroups')}
            </p>
          )}
        </DialogContent>
      )}
    </Dialog>
  )
}
function GroupForm({
  value,
  onClose,
  onSaved,
  restoreFocus,
}: {
  value?: GroupDetail
  onClose: () => void
  onSaved: () => Promise<void>
  restoreFocus: () => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState(value?.name ?? '')
  const [enabled, setEnabled] = useState(value?.enabled ?? true)
  const [selected, setSelected] = useState<string[]>(
    value?.resources
      ?.filter((resource) => resource.kind === 'subscription')
      .map((resource) => resource.id) ??
      value?.account_ids ??
      [],
  )
  const [selectedChannels, setSelectedChannels] = useState<string[]>(
    value?.resources
      ?.filter((resource) => resource.kind === 'channel')
      .map((resource) => resource.id) ?? [],
  )
  const [preference, setPreference] = useState<
    'protocol' | 'subscription_first' | 'api_first'
  >(value?.routing.preference ?? 'subscription_first')
  const [fallback, setFallback] = useState(
    value?.routing.allow_api_fallback ?? false,
  )
  const [sticky, setSticky] = useState(value?.routing.session_sticky ?? false)
  const [restricted, setRestricted] = useState(
    value?.restricted_models ?? false,
  )
  const [models, setModels] = useState(value?.allowed_models.join('\n') ?? '')
  const [invalid, setInvalid] = useState(false)
  const accounts = useQuery(accountOptions)
  const channels = useQuery(channelOptions)
  const mutation = useMutation({ mutationFn: saveGroup, onSuccess: onSaved })
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (mutation.isPending || !accounts.data || !channels.data) return
    const valid = name.trim().length > 0 && Array.from(name.trim()).length <= 64
    setInvalid(!valid)
    if (valid)
      mutation.mutate({
        id: value?.id,
        name: name.trim(),
        enabled,
        resources: [
          ...selected.map((id) => ({ kind: 'subscription' as const, id })),
          ...selectedChannels.map((id) => ({ kind: 'channel' as const, id })),
        ],
        routing: {
          preference,
          allow_api_fallback: fallback,
          session_sticky: sticky,
        },
        model_policy: {
          restricted,
          models: models
            .split(/\r?\n/)
            .map((value) => value.trim())
            .filter(Boolean),
        },
      })
  }
  return (
    <DialogContent
      onCloseAutoFocus={(event) => {
        event.preventDefault()
        restoreFocus()
      }}
      showCloseButton={false}
      className="max-h-[calc(100dvh-2rem)] overflow-y-auto"
      onInteractOutside={(event) => {
        if (mutation.isPending) event.preventDefault()
      }}
      onEscapeKeyDown={(event) => {
        if (mutation.isPending) event.preventDefault()
      }}
    >
      <DialogHeader>
        <DialogTitle>{t(value ? 'editGroup' : 'createGroup')}</DialogTitle>
        <DialogDescription>{t('groupEditorDescription')}</DialogDescription>
      </DialogHeader>
      <form className="space-y-5" onSubmit={submit} noValidate>
        <div className="space-y-2">
          <label htmlFor="group-name" className="text-sm font-medium">
            {t('groupName')}
          </label>
          <Input
            id="group-name"
            value={name}
            maxLength={128}
            onChange={(event) => setName(event.target.value)}
            disabled={mutation.isPending}
            aria-invalid={invalid}
          />
          {invalid && (
            <p role="alert" className="text-sm text-error">
              {t('groupInputInvalid')}
            </p>
          )}
        </div>
        <label className="flex items-center gap-3 text-sm">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(event) => setEnabled(event.target.checked)}
            disabled={mutation.isPending}
            className="size-4 accent-primary focus-visible:ring-2 focus-visible:ring-ring"
          />
          {t('groupEnabled')}
        </label>
        <fieldset className="space-y-2" disabled={mutation.isPending}>
          <legend className="text-sm font-medium">{t('groupAccounts')}</legend>
          <p className="text-xs leading-5 text-muted-foreground">
            {t('groupAccountsHint')}
          </p>
          {accounts.isPending ? (
            <p role="status" className="py-3 text-sm text-muted-foreground">
              {t('loadingAccounts')}
            </p>
          ) : accounts.isError ? (
            <div role="alert" className="space-y-2">
              <p className="text-sm text-error">{t('accountsLoadFailed')}</p>
              <Button
                type="button"
                variant="outline"
                onClick={() => accounts.refetch()}
              >
                {t('reconnect')}
              </Button>
            </div>
          ) : accounts.data.accounts.length === 0 ? (
            <p className="py-3 text-sm text-muted-foreground">
              {t('groupNoAccounts')}
            </p>
          ) : (
            <div className="max-h-60 divide-y divide-border overflow-y-auto rounded-lg border border-border">
              {accounts.data.accounts.map((account) => (
                <label
                  key={account.id}
                  className="flex cursor-pointer items-start gap-3 px-3 py-3 hover:bg-muted"
                >
                  <input
                    type="checkbox"
                    checked={selected.includes(account.id)}
                    onChange={(event) =>
                      setSelected((ids) =>
                        event.target.checked
                          ? [...ids, account.id]
                          : ids.filter((id) => id !== account.id),
                      )
                    }
                    className="mt-0.5 size-4 shrink-0 accent-primary focus-visible:ring-2 focus-visible:ring-ring"
                  />
                  <span className="min-w-0 text-sm">
                    <span className="block break-words font-medium">
                      {account.name}
                    </span>
                    <span className="mt-0.5 block break-words text-xs text-muted-foreground">
                      {providerLabels[account.provider]} · {account.email}
                      {!account.enabled && ` · ${t('disabled')}`}
                    </span>
                  </span>
                </label>
              ))}
            </div>
          )}
        </fieldset>
        <fieldset className="space-y-2" disabled={mutation.isPending}>
          <legend className="text-sm font-medium">{t('channelsTitle')}</legend>
          <p className="text-xs leading-5 text-muted-foreground">
            {t('groupChannelsHint')}
          </p>
          {channels.isPending ? (
            <p role="status" className="py-3 text-sm text-muted-foreground">
              {t('loadingChannels')}
            </p>
          ) : channels.isError ? (
            <div role="alert" className="space-y-2">
              <p className="text-sm text-error">{t('channelsLoadFailed')}</p>
              <Button
                type="button"
                variant="outline"
                onClick={() => channels.refetch()}
              >
                {t('reconnect')}
              </Button>
            </div>
          ) : channels.data.channels.length === 0 ? (
            <div className="space-y-1 py-2">
              <p className="text-sm text-muted-foreground">
                {t('channelsEmpty')}
              </p>
              <Link
                to="/channels"
                onClick={onClose}
                className="text-sm underline underline-offset-4"
              >
                {t('addChannel')}
              </Link>
            </div>
          ) : (
            <div className="max-h-60 divide-y divide-border overflow-y-auto rounded-lg border border-border">
              {channels.data.channels.map((channel) => (
                <label
                  key={channel.id}
                  className="flex cursor-pointer items-start gap-3 px-3 py-3 hover:bg-muted"
                >
                  <input
                    type="checkbox"
                    checked={selectedChannels.includes(channel.id)}
                    onChange={(event) =>
                      setSelectedChannels((ids) =>
                        event.target.checked
                          ? [...ids, channel.id]
                          : ids.filter((id) => id !== channel.id),
                      )
                    }
                    className="mt-0.5 size-4 shrink-0 accent-primary focus-visible:ring-2 focus-visible:ring-ring"
                  />
                  <span className="min-w-0 text-sm">
                    <span className="block break-words font-medium">
                      {channel.name}
                    </span>
                    <span className="mt-0.5 block break-all text-xs text-muted-foreground">
                      {channel.base_url}
                      {!channel.enabled && ` · ${t('disabled')}`}
                    </span>
                  </span>
                </label>
              ))}
            </div>
          )}
        </fieldset>
        <fieldset className="space-y-3" disabled={mutation.isPending}>
          <legend className="text-sm font-medium">{t('groupRouting')}</legend>
          <DropdownMenu modal={false}>
            <DropdownMenuTrigger asChild>
              <Button
                type="button"
                variant="outline"
                className="w-full justify-between"
                aria-label={t('groupRouting')}
              >
                <span>
                  {t(
                    preference === 'subscription_first'
                      ? 'routingSubscriptionFirst'
                      : preference === 'api_first'
                        ? 'routingAPIFirst'
                        : 'routingProtocol',
                  )}
                </span>
                <ChevronDown aria-hidden="true" className="size-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent className="w-(--radix-dropdown-menu-trigger-width)">
              <DropdownMenuRadioGroup
                value={preference}
                onValueChange={(value) =>
                  setPreference(value as typeof preference)
                }
              >
                {(['subscription_first', 'api_first', 'protocol'] as const).map(
                  (mode) => (
                    <DropdownMenuRadioItem key={mode} value={mode}>
                      {t(
                        mode === 'subscription_first'
                          ? 'routingSubscriptionFirst'
                          : mode === 'api_first'
                            ? 'routingAPIFirst'
                            : 'routingProtocol',
                      )}
                    </DropdownMenuRadioItem>
                  ),
                )}
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
          {preference === 'subscription_first' && (
            <label className="flex items-start gap-3 text-sm">
              <input
                type="checkbox"
                checked={fallback}
                onChange={(event) => setFallback(event.target.checked)}
                className="mt-0.5 size-4 shrink-0 accent-primary focus-visible:ring-2 focus-visible:ring-ring"
              />
              <span>{t('allowAPIFallback')}</span>
            </label>
          )}
          <p className="text-xs leading-5 text-muted-foreground">
            {t(
              preference === 'subscription_first'
                ? 'apiFallbackHint'
                : preference === 'api_first'
                  ? 'apiPriorityHint'
                  : 'protocolRoutingHint',
            )}
          </p>
          <label className="flex items-start gap-3 text-sm">
            <input
              type="checkbox"
              checked={sticky}
              onChange={(event) => setSticky(event.target.checked)}
              aria-describedby="group-session-sticky-hint"
              className="mt-0.5 size-4 shrink-0 accent-primary focus-visible:ring-2 focus-visible:ring-ring"
            />
            <span>{t('sessionSticky')}</span>
          </label>
          <p
            id="group-session-sticky-hint"
            className="text-xs leading-5 text-muted-foreground"
          >
            {t('sessionStickyHint')}
          </p>
        </fieldset>
        <fieldset className="space-y-3" disabled={mutation.isPending}>
          <legend className="text-sm font-medium">
            {t('groupModelPolicy')}
          </legend>
          <label className="flex items-center gap-3 text-sm">
            <input
              type="checkbox"
              className="size-4 accent-primary focus-visible:ring-2 focus-visible:ring-ring"
              checked={restricted}
              onChange={(event) => setRestricted(event.target.checked)}
            />
            {t('groupRestrictModels')}
          </label>
          {restricted ? (
            <div className="space-y-2">
              <label htmlFor="group-models" className="text-sm font-medium">
                {t('groupAllowedModels')}
              </label>
              <Textarea
                id="group-models"
                rows={4}
                maxLength={16100}
                spellCheck={false}
                value={models}
                onChange={(event) => setModels(event.target.value)}
                aria-describedby="group-models-hint"
                className="font-mono text-sm"
              />
              <p
                id="group-models-hint"
                className="text-xs leading-5 text-muted-foreground"
              >
                {t('groupModelsHint')}
              </p>
              {!models.trim() && (
                <p role="status" className="text-sm text-warning">
                  {t('groupNoModels')}
                </p>
              )}
            </div>
          ) : (
            <p className="text-xs leading-5 text-muted-foreground">
              {t('groupAllModelsHint')}
            </p>
          )}
        </fieldset>
        {mutation.isError && (
          <p role="alert" className="text-sm text-error">
            {t(groupErrorKey(mutation.error))}
          </p>
        )}
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={onClose}
            disabled={mutation.isPending}
          >
            {t('cancel')}
          </Button>
          <Button
            type="submit"
            disabled={mutation.isPending || !accounts.data || !channels.data}
          >
            {mutation.isPending && (
              <LoaderCircle
                aria-hidden="true"
                className="motion-safe:animate-spin"
              />
            )}
            {t('saveGroup')}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  )
}
