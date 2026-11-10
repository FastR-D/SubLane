import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryHistory } from '@tanstack/react-router'
import { expect, it, vi } from 'vitest'
import { App } from '@/App'
import { createAppRouter } from '@/router'
import { authenticated, memberAuthenticated, workspaces } from '@/test/fixtures'

const policy = {
  enabled: false,
  max_multiplier: 2,
  horizon_fraction: 0.25,
  max_horizon_seconds: 86400,
  curve_power: 2,
  revision: 0,
}
const group = {
  id: 1,
  name: 'Synthetic group',
  enabled: true,
  is_default: false,
  restricted_models: false,
  created_at: 1,
  updated_at: 1,
  account_count: 1,
  member_count: 1,
}
const resource = {
  id: 'synthetic-account',
  name: 'Synthetic account',
  provider: 'codex',
  email: 'member@example.test',
  plan: '',
  enabled: true,
  status: 'ready',
  expires_at: 0,
  created_at: 1,
  updated_at: 1,
}
const weight = {
  account_id: resource.id,
  quota_state: 'available',
  quota_updated_at: 1800000000,
  quota_factor: 0.6,
  load_factor: 1,
  preference_boost: 0,
  effective_weight: 0.6,
  target_share: 1,
  in_flight: 0,
  max_concurrency: 30,
  reset: {
    factor: 1,
    reason: 'disabled',
    window: '',
    reset_at: null,
    window_seconds: null,
    remaining_percent: null,
  },
}
const response = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), { status })

it('shows current scheduling, preserves drafts on conflicts, and previews without saving', async () => {
  let writes = 0
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(authenticated))
      if (url === '/api/workspaces')
        return Promise.resolve(response(workspaces))
      if (url === '/api/accounts')
        return Promise.resolve(response({ accounts: [resource] }))
      if (url === '/api/channels')
        return Promise.resolve(response({ channels: [] }))
      if (url === '/api/groups')
        return Promise.resolve(response({ groups: [group] }))
      if (url === '/api/groups/scheduling-policy' && init?.method === 'PUT') {
        writes++
        return Promise.resolve(
          response({ error: 'scheduling_policy_conflict' }, 409),
        )
      }
      if (url === '/api/groups/scheduling-policy')
        return Promise.resolve(response({ policy, dynamic: true }))
      if (url === '/api/groups/scheduling-policy/revisions')
        return Promise.resolve(response({ revisions: [] }))
      if (url.startsWith('/api/groups/1/scheduling/history'))
        return Promise.resolve(
          response({
            as_of: 1800000000,
            collected_since: 1799900000,
            last_flush: 1800000000,
            dropped: 0,
            truncated: false,
            samples: [],
            usage: [],
            cycles: [
              {
                account_id: resource.id,
                lifecycle: 0,
                scope: ':primary',
                reset_at: 1799999999,
                window_seconds: 3600,
                remaining: 35,
                observation_age: 12,
                observations: 12,
                complete: true,
                policy_revision: 0,
              },
            ],
            evaluation: {
              complete_cycles: 1,
              observed_cycles: 1,
              cohorts: 1,
              reports: [
                {
                  revision: 0,
                  cycles: 1,
                  mean_remaining: 35,
                  comparable: false,
                },
              ],
              recommended_revision: null,
            },
          }),
        )
      if (url.startsWith('/api/groups/1/scheduling'))
        return Promise.resolve(
          response({
            as_of: 1800000000,
            policy,
            dynamic: true,
            weights: [weight],
            resources: [
              {
                account_id: resource.id,
                state: 'eligible',
                tier: 0,
                in_flight: 0,
                max_concurrency: 30,
              },
            ],
          }),
        )
      return Promise.resolve(response({ status: 'ready' }))
    }),
  )
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/groups/scheduling?group=1'] }),
      )}
    />,
  )
  const user = userEvent.setup()
  expect(
    await screen.findByRole('heading', { name: 'Scheduling overview' }),
  ).toBeTruthy()
  expect(await screen.findByText('Synthetic account')).toBeTruthy()
  await user.click(screen.getByRole('button', { name: 'Scheduling overview' }))
  await user.click(
    await screen.findByRole('menuitemradio', { name: 'History' }),
  )
  expect(
    await screen.findByText(
      'Complete cycles: 1 / observed: 1 · comparable account/window cohorts: 1',
    ),
  ).toBeTruthy()
  await user.click(screen.getByText('Observed reset evidence'))
  expect(
    await screen.findByText('Observed 12 seconds before reset'),
  ).toBeTruthy()
  await user.click(screen.getByRole('button', { name: 'Scheduling overview' }))
  await user.click(
    await screen.findByRole('menuitemradio', { name: 'Parameters' }),
  )
  const input = await screen.findByRole('spinbutton', {
    name: 'Maximum reset multiplier (1–2)',
  })
  await user.clear(input)
  await user.type(input, '1.5')
  await user.click(
    screen.getByRole('button', { name: 'Preview draft weights' }),
  )
  await screen.findByRole('heading', { name: 'Preview draft weights' })
  expect(writes).toBe(0)
  await user.click(
    screen.getByRole('button', { name: 'Save scheduling parameters' }),
  )
  expect(await screen.findByRole('alert')).toHaveProperty(
    'textContent',
    'The policy changed. Refresh and review before saving again.',
  )
  expect((input as HTMLInputElement).value).toBe('1.5')
  expect(writes).toBe(1)
})

it('blocks members before scheduling management requests', async () => {
  const calls: string[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string) => {
      calls.push(url)
      if (url === '/api/auth/state')
        return Promise.resolve(response(memberAuthenticated))
      if (url === '/api/workspaces')
        return Promise.resolve(response(workspaces))
      return Promise.resolve(response({ status: 'ready' }))
    }),
  )
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/groups/scheduling'] }),
      )}
    />,
  )
  await screen.findByRole('main')
  expect(calls.some((url) => url.startsWith('/api/groups/'))).toBe(false)
  expect(
    within(screen.getByRole('main')).queryByRole('heading', {
      name: 'Scheduling overview',
    }),
  ).toBeNull()
})
