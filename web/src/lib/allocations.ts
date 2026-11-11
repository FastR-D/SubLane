import { queryOptions, type QueryClient } from '@tanstack/react-query'
import { z } from 'zod'
import { request, ApiError } from './request'
import { authKey, type AuthState } from './auth'

const integer = z.number().int().nonnegative()
const modeSchema = z.enum(['ratio', 'amount', 'tokens', 'windows'])
const unitSchema = z.enum(['amount', 'tokens'])
const rateSchema = z.object({
  model: z.string(),
  input: integer,
  cached: integer,
  output: integer,
})
const windowConditionSchema = z.object({
  duration_seconds: integer,
  limit: integer,
})
const configSchema = z.object({
  mode: modeSchema,
  period: z.enum(['day', 'month', 'durations']),
  reset_time: z
    .string()
    .regex(/^(?:[01][0-9]|2[0-3]):[0-5][0-9]$/)
    .optional(),
  reset_day: z.number().int().min(1).max(31).optional(),
  members: z
    .array(
      z.object({
        user_id: integer,
        limit: integer,
        window_overrides: z.array(windowConditionSchema).max(8).optional(),
      }),
    )
    .max(100),
  rates: z.array(rateSchema).max(4096),
  ratio_unit: unitSchema.optional(),
  windows: z.array(windowConditionSchema).max(8).optional(),
  total: integer.optional(),
})
const priceCoverageSchema = z.object({
  missing_catalog_prices: z.array(z.string()).max(4096).optional(),
  uncovered_models: z.array(z.string()).max(4096).optional(),
  catalog_unavailable: z.boolean().optional(),
})
const revisionSchema = z.object({
  effective_at: integer,
  accounting_anchor_at: integer.optional(),
  config: configSchema,
  price_coverage: priceCoverageSchema.optional(),
})
export const schemeSchema = revisionSchema.extend({
  id: integer,
  name: z.string(),
  group_id: integer,
  group_name: z.string(),
  enabled: z.boolean(),
  created_at: integer,
  edit_effective_at: integer.optional(),
  next: revisionSchema.nullable(),
  write_conditions: z
    .object({
      expected_current_revision_id: integer.nullable(),
      expected_next_revision_id: integer.nullable(),
    })
    .optional(),
})
const balanceSchema = z.object({
  user_id: integer,
  username: z.string(),
  removed: z.boolean().optional(),
  window_kind: z.enum(['day', 'month', 'duration']),
  window_seconds: integer,
  mode: unitSchema,
  limit: integer,
  used: integer,
  tokens: integer,
  pending: integer,
  pending_current: integer,
  in_flight: integer,
  reserved: integer,
  admission_room: integer,
  admission: z.enum(['active', 'risk_limited', 'exhausted', 'unlimited']),
  shared_budget_blocked: z.boolean().optional(),
  reset_at: integer,
})
const pendingSchema = z.object({
  request_id: z.string(),
  user_id: integer,
  mode: unitSchema,
  model: z.string(),
  state: z.string(),
  input: integer,
  output: integer,
  cached: integer,
})
export const allocationDetailSchema = schemeSchema.extend({
  balances: z.array(balanceSchema),
  pending: z.array(pendingSchema).max(256),
  available: z.boolean(),
})
export type Scheme = z.infer<typeof schemeSchema>
export type AllocationDetail = z.infer<typeof allocationDetailSchema>
export type AllocationPending = z.infer<typeof pendingSchema>
export type AllocationMode = z.infer<typeof modeSchema>
export type AllocationConfig = z.infer<typeof configSchema>
export type PriceCoverage = z.infer<typeof priceCoverageSchema>
export type SchemeInput = {
  name: string
  group_id: number
  enabled: boolean
  start_next: boolean
  apply_now?: boolean
  confirm_replacement?: boolean
  expected_current_revision_id?: number | null
  expected_next_revision_id?: number | null
  expected_effective_at?: number
  config: AllocationConfig
}
export const schemesOptions = queryOptions({
  queryKey: ['allocations'],
  refetchInterval: 30_000,
  queryFn: ({ signal }) =>
    request(
      '/api/allocations',
      z.object({ schemes: z.array(schemeSchema).max(32) }),
      { signal },
    ),
})
export function allocationOptions(id: number) {
  return queryOptions({
    queryKey: ['allocation', id],
    queryFn: ({ signal }) =>
      request(`/api/allocations/${id}`, allocationDetailSchema, { signal }),
    refetchInterval: 30_000,
  })
}
export function ownAllocationOptions(client: QueryClient, user: number) {
  return queryOptions({
    queryKey: ['own-allocations', user],
    enabled: () => client.getQueryData<AuthState>(authKey)?.user?.id === user,
    queryFn: ({ signal }) =>
      request(
        '/api/me/allocations',
        z.object({ schemes: z.array(allocationDetailSchema).max(32) }),
        { signal },
      ),
    refetchInterval: 30_000,
  })
}
export function saveScheme({ id, ...input }: SchemeInput & { id?: number }) {
  return request(
    id ? `/api/allocations/${id}` : '/api/allocations',
    schemeSchema,
    { method: id ? 'PATCH' : 'POST', body: JSON.stringify(input) },
  )
}
export function settleAllocation(
  id: number,
  input: {
    request_id: string
    input: number
    output: number
    cached: number
    // Image share of input for image model requests; settled at the image input rate.
    image_input?: number
  },
) {
  return request(`/api/allocations/${id}/settle`, z.undefined(), {
    method: 'POST',
    body: JSON.stringify(input),
  })
}
export const modeLabels = {
  ratio: 'allocationRatio',
  amount: 'allocationAmount',
  tokens: 'allocationTokens',
  windows: 'allocationWindows',
} as const
export function parseAllocationValue(
  value: string,
  mode: AllocationMode,
): number | null {
  const precision = mode === 'ratio' ? 2 : 6
  if (
    !new RegExp(
      `^(?:\\d+(?:\\.\\d{1,${precision}})?|\\.\\d{1,${precision}})$`,
    ).test(value)
  )
    return null
  const [whole, fraction = ''] = value.split('.')
  const n =
    Number(whole || '0') * 10 ** precision +
    Number(fraction.padEnd(precision, '0'))
  return Number.isSafeInteger(n) && n <= 1_000_000_000_000 ? n : null
}
export function allocationValue(n: number, mode: AllocationMode) {
  return String(n / (mode === 'ratio' ? 100 : 1_000_000))
}
export function allocationErrorKey(error: Error) {
  if (error instanceof ApiError) {
    if (
      [
        'allocation_version_required',
        'allocation_version_conflict',
        'allocation_effective_at_required',
        'allocation_effective_at_conflict',
        'allocation_state_conflict',
      ].includes(error.code)
    )
      return 'allocationChanged'
    if (error.code === 'allocation_member_conflict')
      return 'allocationMemberConflict'
    if (error.code === 'allocation_funding_gap') return 'allocationFundingGap'
    if (error.code === 'allocation_replacement_required')
      return 'allocationReplacementRequired'
    if (error.code === 'allocation_initial_schedule_required')
      return 'allocationInitialScheduleRequired'
    if (error.code === 'demo_read_only') return 'demoReadOnly'
    if (
      error.code === 'allocation_pool_conflict' ||
      error.code === 'allocation_pool_locked'
    )
      return 'allocationPoolConflict'
    if (error.code === 'invalid_allocation_input') return 'allocationInvalid'
    if (error.code === 'allocation_unknown_window_override')
      return 'allocationUnknownWindowOverride'
    if (error.code === 'invalid_allocation_settlement')
      return 'allocationSettlementConflict'
    if (error.code === 'allocation_model_unpriced') return 'allocationUnpriced'
    if (error.code === 'allocation_pending') return 'allocationPending'
  }
  return 'allocationFailed'
}

export function deleteScheme(id: number) {
  return request(`/api/allocations/${id}`, z.undefined(), {
    method: 'DELETE',
    body: '{}',
  })
}
export function allocationDeleteErrorKey(error: Error) {
  if (error instanceof ApiError) {
    if (error.code === 'demo_read_only') return 'demoReadOnly'
    if (error.code === 'allocation_in_flight') return 'allocationDeleteInFlight'
    if (error.code === 'allocation_not_found') return 'allocationDeleteMissing'
  }
  return 'allocationDeleteFailed'
}

export function setSchemeEnabled(id: number, enabled: boolean) {
  return request(`/api/allocations/${id}/enabled`, z.undefined(), {
    method: 'POST',
    body: JSON.stringify({ enabled }),
  })
}

const previewSchema = z.object({
  write_conditions: z.object({
    expected_current_revision_id: integer.nullable(),
    expected_next_revision_id: integer.nullable(),
  }),
  proposed_effective_at: integer,
  proposed_accounting_anchor_at: integer,
})

export function previewInitial(input: SchemeInput) {
  return request('/api/allocations/preview', previewSchema, {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function previewScheme(
  id: number,
  action: 'schedule' | 'reschedule' | 'replace',
) {
  return request(`/api/allocations/${id}/preview`, previewSchema, {
    method: 'POST',
    body: JSON.stringify({ action }),
  })
}

export function changeSchedule(
  scheme: Scheme,
  action: 'cancel' | 'reschedule',
  effectiveAt?: number,
) {
  return request(`/api/allocations/${scheme.id}/${action}`, schemeSchema, {
    method: 'POST',
    body: JSON.stringify({
      ...scheme.write_conditions,
      ...(effectiveAt === undefined
        ? {}
        : { expected_effective_at: effectiveAt }),
    }),
  })
}

export function allocationStructure(config: AllocationConfig) {
  return JSON.stringify([
    config.mode,
    config.mode === 'ratio' ? config.ratio_unit : config.mode,
    config.period,
    config.reset_time || '00:00',
    config.period === 'month' ? config.reset_day || 1 : 0,
    (config.windows ?? []).map((w) => w.duration_seconds).sort((a, b) => a - b),
  ])
}

export function availableAllocationPools<
  T extends { id: number; enabled: boolean; account_count: number },
>(pools: T[], schemes: { group_id: number }[]): T[] {
  return pools.filter(
    (pool) =>
      pool.enabled &&
      pool.account_count > 0 &&
      !schemes.some((scheme) => scheme.group_id === pool.id),
  )
}
