import { expect, it } from 'vitest'
import { schedulingTimeline, schedulingPolicySchema } from './scheduling'

it('preserves missing pressure samples and separates quota resets', () => {
  const base = {
    kind: 'quota' as const,
    account_id: 'a',
    group_id: 0,
    bucket: 0,
    scope: ':primary',
    revision: 1,
    observed_at: 0,
    samples: 0,
    in_flight_total: 0,
    peak: 0,
    limit: 0,
    expected: 0,
    selected: 0,
    decisions: 0,
    weight: 0,
    reset_factor: 0,
    remaining: 60,
    reset_at: 1000,
    window_seconds: 3600,
  }
  const rows = schedulingTimeline(
    [
      { ...base, bucket: 300, observed_at: 310 },
      {
        ...base,
        bucket: 600,
        observed_at: 610,
        reset_at: 4600,
        remaining: 100,
      },
    ],
    'quota',
  )
  expect(rows.map((r) => r.value)).toEqual([60, null, 100])
})

it('rejects unbounded scheduling parameters', () => {
  expect(
    schedulingPolicySchema.safeParse({
      enabled: true,
      max_multiplier: 99,
      horizon_fraction: 0.25,
      max_horizon_seconds: 86400,
      curve_power: 2,
      revision: 0,
    }).success,
  ).toBe(false)
})

it('keeps unreported tokens unknown, marks partial reports, and leaves missing hours empty', async () => {
  const { schedulingUsageTimeline } = await import('./scheduling')
  const base = {
    account_id: 'synthetic',
    hour: 3600,
    requests: 2,
    completed: 2,
    errors: 0,
    limited: 0,
    duration_ms: 200,
    wait_ms: 10,
    input_tokens: 40,
    output_tokens: 20,
    input_reported: 1,
    output_reported: 0,
    first_output_ms: 50,
    first_output_reported: 1,
  }
  expect(
    schedulingUsageTimeline([base, { ...base, hour: 10800 }], 'input'),
  ).toEqual([
    { id: '3600', value: 40, partial: true },
    { id: '7200', value: null, partial: false },
    { id: '10800', value: 40, partial: true },
  ])
  expect(schedulingUsageTimeline([base], 'output')[0].value).toBeNull()
  expect(schedulingUsageTimeline([base], 'first_output')[0].value).toBe(50)
})
