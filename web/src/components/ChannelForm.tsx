import { useState, type FormEvent } from 'react'
import { skipToken, useMutation, useQuery } from '@tanstack/react-query'
import { LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  channelErrorKey,
  channelPlatforms,
  channelProtocols,
  saveChannel,
  type Channel,
  type ChannelPlatform,
  type ChannelProtocol,
} from '@/lib/channels'
import { proxyOptions } from '@/lib/proxies'
import { ProxyPicker } from './ProxyPicker'
import { ChannelLogo } from './ChannelLogo'
import { Button } from './ui/Button'
import { Input } from './ui/Input'
import { SearchPicker } from './SearchPicker'
export function ChannelForm({
  channel,
  onCancel,
  onSaved,
  onPendingChange,
}: {
  channel?: Channel
  onCancel: () => void
  onSaved: () => Promise<void>
  onPendingChange?: (pending: boolean) => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState(channel?.name ?? '')
  const [platform, setPlatform] = useState<ChannelPlatform>(
    channel?.platform ?? 'openai',
  )
  const [protocol, setProtocol] = useState<ChannelProtocol>(
    channel?.protocol ?? 'openai',
  )
  const [baseURL, setBaseURL] = useState(
    channel?.base_url ?? 'https://api.openai.com/v1',
  )
  const [key, setKey] = useState('')
  const [proxyID, setProxyID] = useState(channel?.proxy_id ?? '')
  const [invalid, setInvalid] = useState<'name' | 'key' | 'endpoint' | null>(
    null,
  )
  // Keep the shared administrator gate when disabling proxy reads during key rotation.
  const proxies = useQuery({
    ...proxyOptions,
    queryFn: channel ? skipToken : proxyOptions.queryFn,
  })
  const save = useMutation({
    mutationFn: saveChannel,
    gcTime: 0,
    onMutate: () => onPendingChange?.(true),
    onSettled: () => onPendingChange?.(false),
    onSuccess: async () => {
      setKey('')
      await onSaved()
    },
  })
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (save.isPending) return
    setInvalid(null)
    if (
      Array.from(name.trim()).length < 1 ||
      Array.from(name.trim()).length > 64 ||
      /\p{Cc}/u.test(name)
    ) {
      setInvalid('name')
      return
    }
    // Editing keeps the stored key unless a replacement is entered.
    if (
      (!key && !channel) ||
      new TextEncoder().encode(key).length > 16384 ||
      /[\s\p{Cc}]/u.test(key)
    ) {
      setInvalid('key')
      return
    }
    try {
      const url = new URL(baseURL.trim())
      if (
        !['https:', 'http:'].includes(url.protocol) ||
        url.username ||
        url.password ||
        url.search ||
        url.hash ||
        new TextEncoder().encode(baseURL.trim()).length > 2048
      ) {
        setInvalid('endpoint')
        return
      }
    } catch {
      setInvalid('endpoint')
      return
    }
    save.mutate({
      id: channel?.id,
      name: name.trim(),
      api_key: key || undefined,
      base_url: baseURL.trim(),
      platform,
      protocol,
      proxy_id: !channel && proxyID ? proxyID : undefined,
    })
  }
  const fieldErrors = {
    name: 'channelNameInvalid',
    key: 'upstreamAPIKeyInvalid',
    endpoint: 'upstreamBaseURLInvalid',
  } as const
  return (
    <form onSubmit={submit} noValidate className="space-y-5">
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <label htmlFor="channel-platform" className="text-sm font-medium">
            {t('channelPlatform')}
          </label>
          <SearchPicker<ChannelPlatform>
            id="channel-platform"
            label={t('channelPlatform')}
            value={platform}
            onChange={(selected) => {
              setPlatform(selected)
              if (selected !== 'custom') {
                setBaseURL(channelPlatforms[selected].baseURL)
                setProtocol(channelPlatforms[selected].protocol)
              }
            }}
            options={(Object.keys(channelPlatforms) as ChannelPlatform[]).map(
              (value) => ({
                value,
                label: t(channelPlatforms[value].label),
                searchText: `${value} ${channelPlatforms[value].baseURL}`,
                icon: <ChannelLogo platform={value} className="size-5" />,
              }),
            )}
            searchLabel={t('channelPlatformSearch')}
            emptyLabel={t('channelPlatformNoMatch')}
            disabled={Boolean(channel) || save.isPending}
          />
        </div>
        <div className="space-y-2">
          <label htmlFor="channel-protocol" className="text-sm font-medium">
            {t('channelProtocol')}
          </label>
          <SearchPicker<ChannelProtocol>
            id="channel-protocol"
            label={t('channelProtocol')}
            value={protocol}
            onChange={setProtocol}
            options={(Object.keys(channelProtocols) as ChannelProtocol[]).map(
              (value) => ({
                value,
                label: t(channelProtocols[value]),
                searchText: value,
              }),
            )}
            searchLabel={t('channelProtocolSearch')}
            emptyLabel={t('channelProtocolNoMatch')}
            disabled={
              Boolean(channel) || platform !== 'custom' || save.isPending
            }
          />
        </div>
      </div>
      {!channel && (
        <p className="text-xs leading-5 text-muted-foreground">
          {t('channelPlatformHint')}
        </p>
      )}
      <div className="space-y-2">
        <label htmlFor="channel-name" className="text-sm font-medium">
          {t('channelName')}
        </label>
        <Input
          id="channel-name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          disabled={save.isPending}
          maxLength={128}
          aria-invalid={invalid === 'name'}
        />
      </div>
      <div className="space-y-2">
        <label htmlFor="channel-base-url" className="text-sm font-medium">
          {t('upstreamBaseURL')}
        </label>
        <Input
          id="channel-base-url"
          type="url"
          value={baseURL}
          onChange={(event) => setBaseURL(event.target.value)}
          readOnly={Boolean(channel)}
          disabled={save.isPending}
          autoComplete="off"
          spellCheck={false}
          aria-invalid={invalid === 'endpoint'}
          aria-describedby="channel-endpoint-hint"
        />
        <p
          id="channel-endpoint-hint"
          className="text-xs leading-5 text-muted-foreground"
        >
          {t(channel ? 'upstreamEndpointLocked' : 'upstreamBaseURLHint')}
        </p>
      </div>
      <div className="space-y-2">
        <label htmlFor="channel-key" className="text-sm font-medium">
          {t('upstreamAPIKey')}
        </label>
        <Input
          id="channel-key"
          type="password"
          value={key}
          onChange={(event) => setKey(event.target.value)}
          disabled={save.isPending}
          autoComplete="new-password"
          spellCheck={false}
          maxLength={16384}
          aria-invalid={invalid === 'key'}
          aria-describedby="channel-key-hint"
        />
        <p
          id="channel-key-hint"
          className="text-xs leading-5 text-muted-foreground"
        >
          {t(channel ? 'channelKeyKeepHint' : 'channelKeyHint')}
        </p>
      </div>
      {!channel && (
        <div className="space-y-2">
          {proxies.isPending ? (
            <p role="status" className="text-sm text-muted-foreground">
              {t('loadingProxies')}
            </p>
          ) : proxies.isError ? (
            <p role="alert" className="text-sm text-error">
              {t('proxiesLoadFailed')}
            </p>
          ) : (
            <ProxyPicker
              value={proxyID}
              onChange={setProxyID}
              proxies={proxies.data.proxies}
              disabled={save.isPending}
            />
          )}
          <p className="text-xs leading-5 text-muted-foreground">
            {t('apiUpstreamProxyHint')}
          </p>
        </div>
      )}
      {(invalid || save.isError) && (
        <p role="alert" className="text-sm text-error">
          {t(invalid ? fieldErrors[invalid] : channelErrorKey(save.error))}
        </p>
      )}
      <div className="flex flex-col-reverse gap-2 pt-3 sm:flex-row sm:justify-end">
        <Button
          type="button"
          variant="outline"
          onClick={onCancel}
          disabled={save.isPending}
        >
          {t('cancel')}
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending && (
            <LoaderCircle
              className="motion-safe:animate-spin"
              aria-hidden="true"
            />
          )}
          {t(channel ? 'saveChannel' : 'connectAPI')}
        </Button>
      </div>
    </form>
  )
}
