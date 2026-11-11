import { z } from 'zod'
import { ApiError, request } from './request'
import { runtimeSchema } from './runtime'
import { accountErrorKey } from './accounts'

export const channelPlatforms = {
  openai: {
    label: 'channelPlatformOpenAI',
    baseURL: 'https://api.openai.com/v1',
    protocol: 'openai',
  },
  anthropic: {
    label: 'channelPlatformAnthropic',
    baseURL: 'https://api.anthropic.com/v1',
    protocol: 'anthropic',
  },
  gemini: {
    label: 'channelPlatformGemini',
    baseURL: 'https://generativelanguage.googleapis.com/v1beta',
    protocol: 'gemini',
  },
  deepseek: {
    label: 'channelPlatformDeepSeek',
    baseURL: 'https://api.deepseek.com',
    protocol: 'openai',
  },
  openrouter: {
    label: 'channelPlatformOpenRouter',
    baseURL: 'https://openrouter.ai/api/v1',
    protocol: 'openai',
  },
  opencode: {
    label: 'channelPlatformOpenCode',
    baseURL: 'https://opencode.ai/inference/openai/v1',
    protocol: 'openai',
  },
  'opencode-go': {
    label: 'channelPlatformOpenCodeGo',
    baseURL: 'https://opencode.ai/inference/go/openai/v1',
    protocol: 'openai',
  },
  xai: {
    label: 'channelPlatformXAI',
    baseURL: 'https://api.x.ai/v1',
    protocol: 'openai',
  },
  moonshot: {
    label: 'channelPlatformKimi',
    baseURL: 'https://api.moonshot.ai/v1',
    protocol: 'openai',
  },
  'moonshot-cn': {
    label: 'channelPlatformKimiCN',
    baseURL: 'https://api.moonshot.cn/v1',
    protocol: 'openai',
  },
  zhipu: {
    label: 'channelPlatformZhipu',
    baseURL: 'https://open.bigmodel.cn/api/paas/v4',
    protocol: 'openai',
  },
  zai: {
    label: 'channelPlatformZAI',
    baseURL: 'https://api.z.ai/api/paas/v4',
    protocol: 'openai',
  },
  minimax: {
    label: 'channelPlatformMiniMax',
    baseURL: 'https://api.minimax.io/v1',
    protocol: 'openai',
  },
  'minimax-cn': {
    label: 'channelPlatformMiniMaxCN',
    baseURL: 'https://api.minimaxi.com/v1',
    protocol: 'openai',
  },
  qwen: {
    label: 'channelPlatformQwen',
    baseURL: 'https://dashscope-intl.aliyuncs.com/compatible-mode/v1',
    protocol: 'openai',
  },
  'qwen-cn': {
    label: 'channelPlatformQwenCN',
    baseURL: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    protocol: 'openai',
  },
  mistral: {
    label: 'channelPlatformMistral',
    baseURL: 'https://api.mistral.ai/v1',
    protocol: 'openai',
  },
  groq: {
    label: 'channelPlatformGroq',
    baseURL: 'https://api.groq.com/openai/v1',
    protocol: 'openai',
  },
  together: {
    label: 'channelPlatformTogether',
    baseURL: 'https://api.together.xyz/v1',
    protocol: 'openai',
  },
  fireworks: {
    label: 'channelPlatformFireworks',
    baseURL: 'https://api.fireworks.ai/inference/v1',
    protocol: 'openai',
  },
  siliconflow: {
    label: 'channelPlatformSiliconFlow',
    baseURL: 'https://api.siliconflow.cn/v1',
    protocol: 'openai',
  },
  'siliconflow-global': {
    label: 'channelPlatformSiliconFlowGlobal',
    baseURL: 'https://api.siliconflow.com/v1',
    protocol: 'openai',
  },
  nvidia: {
    label: 'channelPlatformNvidia',
    baseURL: 'https://integrate.api.nvidia.com/v1',
    protocol: 'openai',
  },
  modelscope: {
    label: 'channelPlatformModelScope',
    baseURL: 'https://api-inference.modelscope.cn/v1',
    protocol: 'openai',
  },
  azure: { label: 'channelPlatformAzure', baseURL: '', protocol: 'responses' },
  volcengine: {
    label: 'channelPlatformVolcengine',
    baseURL: 'https://ark.cn-beijing.volces.com/api/v3',
    protocol: 'openai',
  },
  baidu: {
    label: 'channelPlatformBaidu',
    baseURL: 'https://qianfan.baidubce.com/v2',
    protocol: 'openai',
  },
  tencent: {
    label: 'channelPlatformTencent',
    baseURL: 'https://tokenhub.tencentmaas.com/v1',
    protocol: 'openai',
  },
  stepfun: {
    label: 'channelPlatformStepFun',
    baseURL: 'https://api.stepfun.com/v1',
    protocol: 'openai',
  },
  xiaomi: {
    label: 'channelPlatformXiaomi',
    baseURL: 'https://api.xiaomimimo.com/v1',
    protocol: 'openai',
  },
  custom: { label: 'channelPlatformCustom', baseURL: '', protocol: 'openai' },
} as const
export const channelProtocols = {
  openai: 'channelProtocolChat',
  responses: 'channelProtocolResponses',
  anthropic: 'channelProtocolAnthropic',
  gemini: 'channelProtocolGemini',
} as const
export type ChannelPlatform = keyof typeof channelPlatforms
export type ChannelProtocol = keyof typeof channelProtocols

export const channelSchema = z.object({
  id: z.string().min(1),
  name: z.string(),
  platform: z
    .enum(
      Object.keys(channelPlatforms) as [ChannelPlatform, ...ChannelPlatform[]],
    )
    .default('custom'),
  protocol: z.enum(['openai', 'responses', 'anthropic', 'gemini']),
  base_url: z.string().max(2048),
  enabled: z.boolean(),
  status: z.enum(['ready', 'unverified', 'key_required']),
  proxy_id: z.string().default(''),
  max_concurrency: z.number().int().min(1).max(1000),
  group_count: z.number().int().nonnegative().optional(),
  created_at: z.number().int().nonnegative(),
  updated_at: z.number().int().nonnegative(),
})
export type Channel = z.infer<typeof channelSchema>
export type ChannelInput = {
  name: string
  api_key?: string
  base_url: string
  proxy_id?: string
  platform?: ChannelPlatform
  protocol?: ChannelProtocol
}
export const channelOptions = {
  queryKey: ['channels'],
  queryFn: ({ signal }: { signal: AbortSignal }) =>
    request(
      '/api/channels',
      z.object({ channels: z.array(channelSchema).max(100) }),
      { signal },
    ),
}
export const channelRuntimeOptions = {
  queryKey: ['channel-runtime'],
  queryFn: ({ signal }: { signal: AbortSignal }) =>
    request(
      '/api/channels/runtime',
      z.object({
        channels: z.array(runtimeSchema).max(100),
        server_time: z.number().int(),
      }),
      { signal },
    ),
  refetchInterval: 5000,
  staleTime: 2000,
}
export function saveChannel({ id, ...input }: ChannelInput & { id?: string }) {
  return request(
    id ? `/api/channels/${encodeURIComponent(id)}` : '/api/channels',
    channelSchema,
    { method: id ? 'PUT' : 'POST', body: JSON.stringify(input) },
  )
}
export function setChannelEnabled({
  id,
  enabled,
}: {
  id: string
  enabled: boolean
}) {
  return request(`/api/channels/${encodeURIComponent(id)}`, channelSchema, {
    method: 'PATCH',
    body: JSON.stringify({ enabled }),
  })
}
export function deleteChannel(id: string) {
  return request(`/api/channels/${encodeURIComponent(id)}`, z.undefined(), {
    method: 'DELETE',
    body: '{}',
  })
}
export function checkChannel(id: string) {
  return request(
    `/api/channels/${encodeURIComponent(id)}/check`,
    z.object({
      channel: channelSchema,
      models: z
        .array(
          z.object({
            id: z.string(),
            object: z.literal('model'),
            owned_by: z.string(),
          }),
        )
        .max(512),
    }),
    { method: 'POST', body: '{}' },
  )
}
export function bindChannelProxy({
  id,
  proxy_id,
}: {
  id: string
  proxy_id: string
}) {
  return request(
    `/api/channels/${encodeURIComponent(id)}/proxy`,
    channelSchema,
    { method: 'PUT', body: JSON.stringify({ proxy_id }) },
  )
}
export function setChannelConcurrency({
  id,
  max_concurrency,
}: {
  id: string
  max_concurrency: number
}) {
  return request(
    `/api/channels/${encodeURIComponent(id)}/limits`,
    channelSchema,
    { method: 'PATCH', body: JSON.stringify({ max_concurrency }) },
  )
}
export function resumeChannel(id: string) {
  return request(
    `/api/channels/${encodeURIComponent(id)}/resume`,
    z.undefined(),
    { method: 'POST', body: '{}' },
  )
}
export function channelErrorKey(error: unknown) {
  if (error instanceof ApiError) {
    if (error.code === 'account_exists') return 'channelExists'
    if (error.code === 'channel_not_found') return 'channelNotFound'
    if (error.code === 'account_reauthorization_required')
      return 'apiKeyReplacementHint'
  }
  return accountErrorKey(error)
}

export function channelLoadErrorKey(error: unknown) {
  // Domain errors such as missing workspaces can also return 404.
  if (
    error instanceof ApiError &&
    error.status === 404 &&
    error.code === 'not_found'
  )
    return 'channelBackendMissing'
  return 'channelsLoadFailed'
}

// Usage is read from opencode.ai only for channels whose key belongs to that origin.
export function reportsOpenCodeUsage(
  channel: Pick<Channel, 'platform' | 'base_url'>,
) {
  if (channel.platform !== 'opencode-go') return false
  try {
    const url = new URL(channel.base_url)
    return url.protocol === 'https:' && url.host === 'opencode.ai'
  } catch {
    return false
  }
}
