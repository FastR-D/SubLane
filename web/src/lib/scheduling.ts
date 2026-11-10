import { queryOptions } from '@tanstack/react-query'
import { z } from 'zod'
import { request } from './request'
import {
  schedulingPolicySchema,
  schedulingSchema,
  type SchedulingPolicy,
} from './groups'

export { schedulingPolicySchema }
export const schedulingPolicyOptions = queryOptions({
  queryKey: ['scheduling-policy'],
  queryFn: ({ signal }) =>
    request(
      '/api/groups/scheduling-policy',
      z.object({ policy: schedulingPolicySchema, dynamic: z.boolean() }),
      { signal },
    ),
})
export const schedulingRevisionOptions = queryOptions({
  queryKey: ['scheduling-revisions'],
  queryFn: ({ signal }) =>
    request(
      '/api/groups/scheduling-policy/revisions',
      z.object({
        revisions: z
          .array(
            schedulingPolicySchema.extend({
              created_at: z.number().int().nonnegative(),
            }),
          )
          .max(50),
      }),
      { signal },
    ),
})
export function saveSchedulingPolicy(policy: SchedulingPolicy) {
  return request('/api/groups/scheduling-policy', schedulingPolicySchema, {
    method: 'PUT',
    body: JSON.stringify(policy),
  })
}
export function previewSchedulingPolicy(
  group: number,
  model: string,
  policy: SchedulingPolicy,
) {
  return request(`/api/groups/${group}/scheduling/preview`, schedulingSchema, {
    method: 'POST',
    body: JSON.stringify({ model, policy }),
  })
}
const sampleSchema = z.object({
  kind: z.enum(['pressure', 'quota', 'placement']),
  account_id: z.string().min(1),
  group_id: z.number().int().nonnegative(),
  bucket: z.number().int().nonnegative(),
  scope: z.string().max(256),
  revision: z.number().int().nonnegative(),
  observed_at: z.number().int().nonnegative(),
  samples: z.number().int().nonnegative(),
  in_flight_total: z.number().int().nonnegative(),
  peak: z.number().int().nonnegative(),
  limit: z.number().int().nonnegative(),
  expected: z.number().nonnegative(),
  selected: z.number().int().nonnegative(),
  decisions: z.number().int().nonnegative(),
  weight: z.number().nonnegative(),
  reset_factor: z.number().min(0).max(2),
  remaining: z.number().min(0).max(100).nullable(),
  reset_at: z.number().int().nullable(),
  window_seconds: z.number().int().nullable(),
})
const usageSchema = z.object({
  account_id: z.string(),
  hour: z.number().int(),
  requests: z.number().int().nonnegative(),
  completed: z.number().int().nonnegative(),
  errors: z.number().int().nonnegative(),
  limited: z.number().int().nonnegative(),
  duration_ms: z.number().int().nonnegative(),
  wait_ms: z.number().int().nonnegative(),
  input_tokens: z.number().int().nonnegative(),
  output_tokens: z.number().int().nonnegative(),
  input_reported: z.number().int().nonnegative(),
  output_reported: z.number().int().nonnegative(),
  first_output_ms: z.number().int().nonnegative(),
  first_output_reported: z.number().int().nonnegative(),
})
const cycleSchema = z.object({
  account_id: z.string(),
  lifecycle: z.number().int().nonnegative(),
  scope: z.string(),
  reset_at: z.number().int(),
  window_seconds: z.number().int().positive(),
  remaining: z.number().min(0).max(100),
  observation_age: z.number().int().nonnegative(),
  observations: z.number().int().nonnegative(),
  complete: z.boolean(),
  policy_revision: z.number().int().nonnegative(),
})
const historySchema = z.object({
  cycles: z.array(cycleSchema).max(499),
  evaluation: z.object({
    complete_cycles: z.number().int().nonnegative(),
    observed_cycles: z.number().int().nonnegative(),
    cohorts: z.number().int().nonnegative(),
    reports: z
      .array(
        z.object({
          revision: z.number().int().nonnegative(),
          cycles: z.number().int().nonnegative(),
          mean_remaining: z.number().min(0).max(100),
          comparable: z.boolean(),
        }),
      )
      .max(499),
    recommended_revision: z.number().int().nonnegative().nullable(),
  }),
  as_of: z.number().int().nonnegative(),
  collected_since: z.number().int().nonnegative(),
  last_flush: z.number().int().nonnegative(),
  dropped: z.number().int().nonnegative(),
  truncated: z.boolean(),
  samples: z.array(sampleSchema).max(2000),
  usage: z.array(usageSchema).max(2000),
})
export type SchedulingSample = z.infer<typeof sampleSchema>
export type SchedulingHistory = z.infer<typeof historySchema>
export function schedulingHistoryOptions(
  group: number,
  hours: number,
  account: string,
) {
  return queryOptions({
    queryKey: ['scheduling-history', group, hours, account],
    queryFn: ({ signal }) =>
      request(
        `/api/groups/${group}/scheduling/history?hours=${hours}&account=${encodeURIComponent(account)}`,
        historySchema,
        { signal },
      ),
  })
}

export function schedulingTimeline(
  samples: SchedulingSample[],
  kind: 'pressure' | 'quota' | 'placement',
) {
  const values = samples
    .filter((v) => v.kind === kind)
    .sort((a, b) => a.observed_at - b.observed_at || a.revision - b.revision)
    .filter((v, index, rows) => rows[index + 1]?.observed_at !== v.observed_at)
  const result: { id: string; value: number | null }[] = []
  let last: SchedulingSample | undefined
  for (const v of values) {
    if (
      last &&
      ((kind === 'quota' && last.reset_at !== v.reset_at) ||
        (kind !== 'pressure' && last.revision !== v.revision) ||
        v.observed_at - last.observed_at >
          (kind === 'pressure' ? 120 : kind === 'quota' ? 600 : 7200))
    ) {
      result.push({
        id: String((last.observed_at + v.observed_at) / 2),
        value: null,
      })
    }
    result.push({
      id: String(v.observed_at),
      value:
        kind === 'quota'
          ? v.remaining
          : kind === 'placement'
            ? v.weight
            : v.samples > 0
              ? v.in_flight_total / v.samples
              : null,
    })
    last = v
  }
  return result
}

export function schedulingUsageTimeline(
  usage: SchedulingHistory['usage'],
  metric: string,
) {
  const result: { id: string; value: number | null; partial: boolean }[] = []
  let previous: number | undefined
  for (const v of [...usage].sort((a, b) => a.hour - b.hour)) {
    if (previous !== undefined && v.hour - previous > 3600)
      result.push({ id: String(previous + 3600), value: null, partial: false })
    const input = metric === 'input',
      output = metric === 'output'
    const reports = input ? v.input_reported : output ? v.output_reported : 0
    const value =
      input || output
        ? reports > 0
          ? input
            ? v.input_tokens
            : v.output_tokens
          : null
        : metric === 'requests'
          ? v.requests
          : metric === 'errors'
            ? v.errors
            : metric === 'limited'
              ? v.limited
              : metric === 'first_output'
                ? v.first_output_reported
                  ? v.first_output_ms / v.first_output_reported
                  : null
                : v.requests
                  ? (metric === 'wait' ? v.wait_ms : v.duration_ms) / v.requests
                  : null
    result.push({
      id: String(v.hour),
      value,
      partial: (input || output) && reports < v.requests,
    })
    previous = v.hour
  }
  return result
}
