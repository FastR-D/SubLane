import { render, screen, within, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryHistory } from '@tanstack/react-router'
import { expect, it, vi } from 'vitest'
import { App } from '@/App'
import { createAppRouter } from '@/router'
import { authenticated } from '@/test/fixtures'

const base = {
  restricted_models: false,
  id: 1,
  name: 'Default',
  is_default: false,
  enabled: true,
  account_count: 1,
  member_count: 1,
  created_at: 1,
  updated_at: 1,
}
const account = {
  id: 'synthetic-account',
  provider: 'codex',
  name: 'Synthetic account',
  email: 'member@example.test',
  plan: '',
  enabled: true,
  status: 'ready',
  expires_at: 0,
  created_at: 1,
  updated_at: 1,
}
const response = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), { status })

it('selects subscriptions and channels separately with API fallback off by default', async () => {
  const channel = {
    id: 'synthetic-channel',
    name: 'Synthetic channel',
    protocol: 'openai',
    base_url: 'https://relay.example.test/v1',
    enabled: true,
    status: 'ready',
    max_concurrency: 30,
    created_at: 1,
    updated_at: 1,
  }
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(authenticated))
      if (url === '/api/accounts')
        return Promise.resolve(response({ accounts: [account] }))
      if (url === '/api/channels')
        return Promise.resolve(response({ channels: [channel] }))
      if (url === '/api/groups' && init?.method === 'POST') {
        const input = JSON.parse(String(init.body))
        expect(input.resources).toEqual([
          { kind: 'subscription', id: account.id },
          { kind: 'channel', id: channel.id },
        ])
        expect(input.routing).toEqual({
          preference: 'subscription_first',
          allow_api_fallback: false,
          session_sticky: false,
        })
        return Promise.resolve(
          response({
            ...base,
            ...input,
            account_count: 2,
            account_ids: [account.id, channel.id],
            allowed_models: [],
          }),
        )
      }
      return Promise.resolve(response({ groups: [] }))
    }),
  )
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/groups'] }),
      )}
    />,
  )
  const user = userEvent.setup()
  await user.click(
    await screen.findByRole('button', { name: 'Create resource group' }),
  )
  const dialog = await screen.findByRole('dialog')
  await user.type(
    within(dialog).getByLabelText('Group name'),
    'Synthetic mixed resources',
  )
  await user.click(
    await within(dialog).findByRole('checkbox', { name: /Synthetic account/ }),
  )
  await user.click(
    await within(dialog).findByRole('checkbox', { name: /Synthetic channel/ }),
  )
  expect(
    (
      within(dialog).getByRole('checkbox', {
        name: 'Allow API fallback when subscriptions are unavailable',
      }) as HTMLInputElement
    ).checked,
  ).toBe(false)
  await user.click(within(dialog).getByRole('button', { name: 'Save group' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
})
it('creates a pool with only selected accounts', async () => {
  const groups = [base]
  const fetch = vi
    .fn()
    .mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/channels')
        return Promise.resolve(response({ channels: [] }))
      if (url === '/api/auth/state')
        return Promise.resolve(response(authenticated))
      if (url === '/api/accounts')
        return Promise.resolve(response({ accounts: [account] }))
      if (url === '/api/groups' && init?.method === 'POST') {
        expect(JSON.parse(String(init.body))).toEqual({
          name: 'Project alpha',
          enabled: true,
          resources: [{ kind: 'subscription', id: 'synthetic-account' }],
          routing: {
            preference: 'subscription_first',
            allow_api_fallback: false,
            session_sticky: false,
          },
          model_policy: { restricted: false, models: [] },
        })
        const group = {
          ...base,
          id: 2,
          name: 'Project alpha',
          is_default: false,
          member_count: 0,
        }
        groups.push(group)
        return Promise.resolve(
          response(
            {
              ...group,
              account_ids: ['synthetic-account'],
              allowed_models: [],
            },
            201,
          ),
        )
      }
      if (url === '/api/groups') return Promise.resolve(response({ groups }))
      return Promise.reject(new Error('Unexpected request'))
    })
  vi.stubGlobal('fetch', fetch)
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/groups'] }),
      )}
    />,
  )
  await screen.findByRole('heading', { name: 'Resource groups' })
  const user = userEvent.setup()
  await user.click(
    screen.getByRole('button', { name: 'Create resource group' }),
  )
  const dialog = await screen.findByRole('dialog')
  await user.type(within(dialog).getByLabelText('Group name'), 'Project alpha')
  await user.click(
    await within(dialog).findByRole('checkbox', { name: /Synthetic account/ }),
  )
  await user.click(within(dialog).getByRole('button', { name: 'Save group' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  await screen.findByText('Project alpha')
})

it('edits an exact model allowlist and makes an empty list explicitly deny all', async () => {
  const fetch = vi
    .fn()
    .mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/channels')
        return Promise.resolve(response({ channels: [] }))
      if (url === '/api/auth/state')
        return Promise.resolve(response(authenticated))
      if (url === '/api/accounts')
        return Promise.resolve(response({ accounts: [account] }))
      if (url === '/api/groups/1') {
        if (init?.method === 'PATCH') {
          expect(JSON.parse(String(init.body)).model_policy).toEqual({
            restricted: true,
            models: [],
          })
        }
        return Promise.resolve(
          response({
            ...base,
            restricted_models: true,
            allowed_models: ['codex/synthetic-model'],
            account_ids: [account.id],
          }),
        )
      }
      return Promise.resolve(
        response({ groups: [{ ...base, restricted_models: true }] }),
      )
    })
  vi.stubGlobal('fetch', fetch)
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/groups'] }),
      )}
    />,
  )
  const user = userEvent.setup()
  await user.click(await screen.findByRole('button', { name: 'Edit Default' }))
  const dialog = await screen.findByRole('dialog')
  await user.clear(await within(dialog).findByLabelText('Allowed model IDs'))
  expect(
    within(dialog).getByText('No models are allowed while this list is empty.'),
  ).toBeTruthy()
  await user.click(within(dialog).getByRole('button', { name: 'Save group' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
})

it('shows a renamed first pool and allows editing its name and enabled state', async () => {
  const pool = { ...base, name: 'Synthetic renamed pool', is_default: false }
  const fetch = vi
    .fn()
    .mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/channels')
        return Promise.resolve(response({ channels: [] }))
      if (url === '/api/auth/state')
        return Promise.resolve(response(authenticated))
      if (url === '/api/accounts')
        return Promise.resolve(response({ accounts: [account] }))
      if (url === '/api/groups')
        return Promise.resolve(response({ groups: [pool] }))
      if (url === '/api/groups/1') {
        if (init?.method === 'PATCH') {
          Object.assign(pool, JSON.parse(String(init.body)))
        }
        return Promise.resolve(
          response({ ...pool, account_ids: [account.id], allowed_models: [] }),
        )
      }
      return Promise.reject(new Error('Unexpected request'))
    })
  vi.stubGlobal('fetch', fetch)
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/groups'] }),
      )}
    />,
  )
  const user = userEvent.setup()
  await user.click(
    await screen.findByRole('button', { name: 'Edit Synthetic renamed pool' }),
  )
  const dialog = await screen.findByRole('dialog')
  await user.clear(await within(dialog).findByLabelText('Group name'))
  await user.type(
    within(dialog).getByLabelText('Group name'),
    'Synthetic updated pool',
  )
  await user.click(within(dialog).getByLabelText('Group enabled'))
  await user.click(within(dialog).getByRole('button', { name: 'Save group' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  expect(pool.name).toBe('Synthetic updated pool')
  expect(pool.enabled).toBe(false)
})
