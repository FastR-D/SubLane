import { z } from 'zod'
import { ApiError, request } from './request'

export const proxySchema = z.object({
  id: z.string().min(1),
  name: z.string(),
  endpoint: z.string(),
  account_count: z.number().int().nonnegative(),
  created_at: z.number().int().nonnegative(),
  updated_at: z.number().int().nonnegative(),
  checked_at: z.number().int().nonnegative().default(0),
  reachable: z.boolean().default(false),
  exit_ip: z.string().max(45).default(''),
  country: z.string().max(2).default(''),
  region: z.string().max(128).default(''),
  city: z.string().max(128).default(''),
  latency_ms: z.number().int().nonnegative().default(0),
  check_error: z.string().max(32).default(''),
  prune_eligible: z.boolean().default(false),
})
export type Proxy = z.infer<typeof proxySchema>

export const proxyOptions = {
  queryKey: ['proxies'],
  queryFn: ({ signal }: { signal: AbortSignal }) =>
    request(
      '/api/proxies',
      z.object({ proxies: z.array(proxySchema).max(32) }),
      {
        signal,
      },
    ),
}

export function saveProxy(input: { id?: string; name: string; url: string }) {
  return request(
    input.id ? `/api/proxies/${encodeURIComponent(input.id)}` : '/api/proxies',
    proxySchema,
    {
      method: input.id ? 'PUT' : 'POST',
      body: JSON.stringify({ name: input.name, url: input.url }),
    },
  )
}

export function deleteProxy(id: string) {
  return request(`/api/proxies/${encodeURIComponent(id)}`, z.undefined(), {
    method: 'DELETE',
    body: '{}',
  })
}

export function importProxies(text: string) {
  return request(
    '/api/proxies/import',
    z.object({ proxies: z.array(proxySchema).max(32) }),
    {
      method: 'POST',
      body: JSON.stringify({ text }),
    },
  )
}

export function checkProxy(id: string) {
  return request(`/api/proxies/${encodeURIComponent(id)}/check`, proxySchema, {
    method: 'POST',
    body: '{}',
  })
}

export function pruneFailedProxies() {
  return request(
    '/api/proxies/prune',
    z.object({ deleted_count: z.number().int().nonnegative() }),
    {
      method: 'POST',
      body: '{}',
    },
  )
}

const errors = {
  demo_read_only: 'demoReadOnly',
  invalid_proxy_input: 'proxyInvalidInput',
  proxy_not_found: 'proxyNotFound',
  proxy_in_use: 'proxyInUse',
  proxy_exists: 'proxyExists',
  proxy_limit: 'proxyLimit',
  proxy_changed: 'proxyChanged',
} as const
export function proxyErrorKey(error: unknown) {
  const code = error instanceof ApiError ? error.code : ''
  return Object.hasOwn(errors, code)
    ? errors[code as keyof typeof errors]
    : 'proxyActionFailed'
}

export function proxyImportErrorKey(error: unknown) {
  if (error instanceof ApiError) {
    if (error.status === 404 && error.code === 'not_found')
      return 'proxyImportUnavailable'
    if (error.code === 'invalid_proxy_input') return 'proxyImportInvalid'
  }
  return proxyErrorKey(error)
}

const ipReportSchema = z.object({
  ip: z.string().max(45),
  trust_score: z.number().int().min(0).max(100).nullable(),
  company_type: z.string().max(32).default(''),
  residential: z.boolean(),
  datacenter: z.boolean(),
  mobile: z.boolean(),
  vpn: z.boolean(),
  proxy: z.boolean(),
  tor: z.boolean(),
  abuser: z.boolean(),
  crawler: z.boolean(),
  asn: z.number().int().nonnegative(),
  as_organization: z.string().max(128).default(''),
  isp: z.string().max(128).default(''),
  country_code: z.string().max(2).default(''),
  country: z.string().max(64).default(''),
  region: z.string().max(64).default(''),
  city: z.string().max(64).default(''),
  verdict: z.string().max(48).default(''),
})
export type IPReport = z.infer<typeof ipReportSchema>

// Reading only returns a report the server already holds; it never discloses the exit IP.
// The key changes when a check observes a new exit.
export function ipReportOptions(id: string, ip: string) {
  return {
    queryKey: ['proxy-ip-report', id, ip],
    queryFn: async ({ signal }: { signal: AbortSignal }) => {
      try {
        return await request(
          `/api/proxies/${encodeURIComponent(id)}/ip-report`,
          ipReportSchema,
          { signal },
        )
      } catch (error) {
        if (error instanceof ApiError && error.code === 'ip_report_not_loaded')
          return null
        throw error
      }
    },
    staleTime: 6 * 60 * 60 * 1000,
    refetchOnWindowFocus: false,
    retry: false,
  }
}

// Explicit administrator action: sends the proxy's exit IP to ip.net.coffee.
export function refreshIPReport(id: string) {
  return request(
    `/api/proxies/${encodeURIComponent(id)}/ip-report/refresh`,
    ipReportSchema,
    { method: 'POST', body: '{}' },
  )
}

export function ipReportErrorKey(error: unknown) {
  if (error instanceof ApiError) {
    if (error.code === 'ip_report_rate_limited') return 'ipReportRateLimited'
    if (error.code === 'ip_report_unsupported') return 'ipReportUnsupported'
  }
  return 'ipReportFailed'
}

// Tiers match ip.net.coffee's scoring page so a card and the full report agree.
export function ipScoreTier(score: number | null) {
  if (score === null)
    return { tone: 'neutral', label: 'ipReportScoreUnknown' } as const
  if (score >= 95)
    return { tone: 'success', label: 'ipReportPristine' } as const
  if (score >= 80) return { tone: 'success', label: 'ipReportClean' } as const
  if (score >= 50) return { tone: 'info', label: 'ipReportGood' } as const
  if (score >= 25) return { tone: 'warning', label: 'ipReportNeutral' } as const
  return { tone: 'error', label: 'ipReportSuspicious' } as const
}
