import { withPage } from '@/test/fixtures'
import { act, render, screen, within, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryHistory } from '@tanstack/react-router'
import { expect, it, vi } from 'vitest'
import { App } from '@/App'
import { createAppRouter } from '@/router'
import { authOptions } from '@/lib/auth'
import * as queries from '@/lib/query'
import { memberAuthenticated } from '@/test/fixtures'
import { i18n } from '@/lib/i18n'

const secret = 'sl_' + 'x'.repeat(43)
const metadata = {
  copyable: true,
  enabled: true,
  expires_at: null as number | null,
  id: 1,
  group_id: 1,
  group_name: 'Default',
  group_access: 'allowed',
  name: 'Synthetic laptop',
  prefix: 'sl_xxxxxxxx',
  created_at: 1900000000,
  last_used_at: null,
  revoked_at: null as number | null,
}
const response = (value: unknown) =>
  new Response(
    JSON.stringify(
      value && typeof value === 'object' && 'keys' in value
        ? withPage({ server_time: 1900000000, ...value })
        : value,
    ),
  )

it('configures a specific personal key from its permitted model catalog', async () => {
  const calls: string[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      calls.push(url)
      return Promise.resolve(
        response(
          url === '/api/auth/state'
            ? memberAuthenticated
            : url === '/api/connection'
              ? {
                  status: 'ready',
                  setup: { stage: 'client', has_successful_request: false },
                }
              : url === '/api/keys/1/models'
                ? {
                    models: [
                      {
                        id: 'synthetic-allowed',
                        object: 'model',
                        owned_by: 'codex',
                      },
                    ],
                    known_accounts: 1,
                    unknown_accounts: 0,
                    stale_accounts: 0,
                    refreshing: false,
                    refresh_failed: false,
                    server_time: 1900000000,
                  }
                : { keys: [metadata], next_cursor: 0 },
        ),
      )
    }),
  )
  const user = userEvent.setup()
  open()
  await screen.findByRole('heading', { name: 'API keys' })
  const ccSwitch = await screen.findByRole('button', {
    name: 'Import Synthetic laptop into CC Switch',
  })
  const magpie = screen.getByRole('button', {
    name: 'Import Synthetic laptop into Magpie',
  })
  expect(ccSwitch.nextElementSibling).toBe(magpie)
  await user.click(
    await screen.findByRole('button', { name: 'Configure Synthetic laptop' }),
  )
  const picker = await screen.findByRole('combobox', { name: 'Model ID' })
  await user.click(picker)
  await user.click(
    await screen.findByRole('option', { name: 'synthetic-allowed' }),
  )
  expect(screen.getByText(/model = "synthetic-allowed"/)).toBeTruthy()
  expect(calls).toContain('/api/keys/1/models')
  expect(calls.some((url) => url.includes('/reveal'))).toBe(false)
})
function open() {
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/keys'] }),
      )}
    />,
  )
}

it('restores an existing key through its allowance without disclosing or replacing its secret', async () => {
  let value = {
    ...metadata,
    group_access: 'blocked',
    allocation_required: true,
    can_restore: true,
  }
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    if (url === '/api/auth/state')
      return Promise.resolve(response(memberAuthenticated))
    if (url === '/api/keys/1/group' && init?.method === 'PUT') {
      expect(JSON.parse(String(init.body))).toEqual({ group_id: 1 })
      value = {
        ...value,
        group_access: 'allowed',
        allocation_required: false,
        can_restore: false,
      }
      return Promise.resolve(response(value))
    }
    return Promise.resolve(response({ keys: [value], next_cursor: 0 }))
  })
  vi.stubGlobal('fetch', fetch)
  const user = userEvent.setup()
  open()
  await user.click(
    await screen.findByRole('button', {
      name: 'Restore access for Synthetic laptop',
    }),
  )
  const dialog = await screen.findByRole('dialog', { name: 'Restore access' })
  expect(
    within(dialog).getByText(/Your key and client configuration stay the same/),
  ).toBeTruthy()
  await user.click(
    within(dialog).getByRole('button', { name: 'Restore access' }),
  )
  await screen.findByText('Access restored for “Synthetic laptop”.')
  expect(
    screen.queryByRole('button', {
      name: 'Restore access for Synthetic laptop',
    }),
  ).toBeNull()
  expect(fetch.mock.calls.some(([url]) => url.endsWith('/secret'))).toBe(false)
  expect(screen.queryByDisplayValue(secret)).toBeNull()
  await act(() => i18n.changeLanguage('zh'))
  expect(screen.getByText('“Synthetic laptop”已恢复使用。')).toBeTruthy()
  await act(() => i18n.changeLanguage('en'))
})

it('lets a member choose an authorized resource group and retains a rejected selection for retry', async () => {
  let value = { ...metadata }
  let reject = true
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    if (url === '/api/auth/state')
      return Promise.resolve(response(memberAuthenticated))
    if (url === '/api/keys/groups')
      return Promise.resolve(
        response({
          groups: [
            { id: 1, name: 'Default', account_count: 1 },
            {
              id: 2,
              name: 'Synthetic destination',
              account_count: 1,
              scheme_id: 7,
              scheme_name: 'Synthetic allowance',
            },
          ],
        }),
      )
    if (url === '/api/keys/1/group' && init?.method === 'PUT') {
      expect(JSON.parse(String(init.body))).toEqual({ group_id: 2 })
      if (reject) {
        reject = false
        return Promise.resolve(
          new Response(JSON.stringify({ error: 'group_unavailable' }), {
            status: 403,
          }),
        )
      }
      value = { ...value, group_id: 2, group_name: 'Synthetic destination' }
      return Promise.resolve(response(value))
    }
    return Promise.resolve(response({ keys: [value], next_cursor: 0 }))
  })
  vi.stubGlobal('fetch', fetch)
  const user = userEvent.setup()
  open()
  await user.click(
    await screen.findByRole('button', {
      name: 'Change resource group for Synthetic laptop',
    }),
  )
  const dialog = await screen.findByRole('dialog', { name: 'Change group' })
  await user.click(
    await within(dialog).findByRole('button', { name: 'Resource group' }),
  )
  await user.click(
    await screen.findByRole('menuitemradio', { name: /Synthetic destination/ }),
  )
  await user.click(within(dialog).getByRole('button', { name: 'Save changes' }))
  await within(dialog).findByRole('alert')
  expect(screen.getByRole('dialog')).toBe(dialog)
  await user.click(within(dialog).getByRole('button', { name: 'Save changes' }))
  await screen.findByText('Resource group updated for “Synthetic laptop”.')
  expect(await screen.findByText('Group: Synthetic destination')).toBeTruthy()
  expect(fetch.mock.calls.some(([url]) => url.endsWith('/secret'))).toBe(false)
})

it('explains when an unbound key needs an administrator instead of offering an unusable recovery action', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) =>
      Promise.resolve(
        response(
          url === '/api/auth/state'
            ? memberAuthenticated
            : {
                keys: [
                  {
                    ...metadata,
                    group_access: 'blocked',
                    allocation_required: true,
                    can_restore: false,
                  },
                ],
                next_cursor: 0,
              },
        ),
      ),
    ),
  )
  open()
  await screen.findByText(
    /Ask a workspace administrator to include you in an active allowance/,
  )
  expect(
    screen.queryByRole('button', {
      name: 'Restore access for Synthetic laptop',
    }),
  ).toBeNull()
})

it('resets pagination in both directions and keeps filtered pages in separate caches', async () => {
  const calls: string[] = []
  const active = { ...metadata, id: 4, name: 'Synthetic active' }
  const paused = {
    ...metadata,
    id: 2,
    name: 'Synthetic paused',
    enabled: false,
  }
  const revoked = {
    ...metadata,
    id: 8,
    name: 'Synthetic revoked',
    revoked_at: 1900000000,
    copyable: false,
  }
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(memberAuthenticated))
      calls.push(url)
      const params = new URL(url, 'http://example.test').searchParams
      const pageNumber = Number(params.get('page') || 1)
      const includeRevoked = params.get('include_revoked') === 'true'
      const page = includeRevoked
        ? pageNumber === 1
          ? { keys: [revoked], next_cursor: 8 }
          : { keys: [active], next_cursor: 0 }
        : pageNumber === 1
          ? { keys: [active], next_cursor: 3 }
          : { keys: [paused], next_cursor: 0 }
      return Promise.resolve(
        response({ ...page, total: 51, page: pageNumber, page_size: 50 }),
      )
    }),
  )
  const user = userEvent.setup()
  open()
  await screen.findByText('Synthetic active')
  const toggle = screen.getByRole('switch', {
    name: 'Show revoked',
  }) as HTMLInputElement
  expect(toggle.checked).toBe(false)
  await user.click(screen.getByRole('button', { name: 'Next' }))
  await screen.findByText('Synthetic paused')
  await user.click(toggle)
  await screen.findByText('Synthetic revoked')
  expect(
    screen.getByRole('button', { name: 'Previous' }).hasAttribute('disabled'),
  ).toBe(true)
  expect(
    screen
      .getByRole('button', { name: 'Edit Synthetic revoked' })
      .hasAttribute('disabled'),
  ).toBe(true)
  await user.click(screen.getByRole('button', { name: 'Next' }))
  await screen.findByText('Synthetic active')
  await user.click(toggle)
  await screen.findByText('Synthetic active')
  expect(
    screen.getByRole('button', { name: 'Previous' }).hasAttribute('disabled'),
  ).toBe(true)
  expect(screen.queryByText('Synthetic revoked')).toBeNull()
  expect(calls).toContain('/api/keys?page=1&limit=50&include_revoked=false')
  expect(calls).toContain('/api/keys?page=2&limit=50&include_revoked=false')
  expect(calls).toContain('/api/keys?page=1&limit=50&include_revoked=true')
  expect(calls).toContain('/api/keys?page=2&limit=50&include_revoked=true')
})

it('keeps the revoked toggle available in empty and failed filtered views', async () => {
  let fail = true
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(memberAuthenticated))
      if (fail && url.includes('include_revoked=true'))
        return Promise.resolve(new Response('{}', { status: 503 }))
      return Promise.resolve(response({ keys: [], next_cursor: 0 }))
    }),
  )
  const user = userEvent.setup()
  open()
  await screen.findByText('No unrevoked API keys')
  expect(
    screen.getByText(
      'Create a personal key, or turn on Show revoked to view past keys.',
    ),
  ).toBeTruthy()
  const toggle = screen.getByRole('switch', { name: 'Show revoked' })
  await user.click(toggle)
  await screen.findByText('Could not load your API keys. Please try again.')
  await user.click(toggle)
  await screen.findByText('No unrevoked API keys')
  fail = false
  await user.click(toggle)
  await screen.findByText('No API keys yet')
})

it('returns to the first page after revoking the last key on a later page', async () => {
  let revoked = false
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(memberAuthenticated))
      if (url.endsWith('/revoke')) {
        revoked = true
        return Promise.resolve(
          response({
            ...metadata,
            id: 2,
            revoked_at: 1900000000,
            copyable: false,
          }),
        )
      }
      const pageNumber = Number(
        new URL(url, 'http://example.test').searchParams.get('page') || 1,
      )
      return Promise.resolve(
        response(
          pageNumber === 1
            ? {
                keys: [{ ...metadata, id: 4, name: 'Synthetic first' }],
                next_cursor: revoked ? 0 : 3,
                total: revoked ? 1 : 51,
                page: 1,
                page_size: 50,
              }
            : {
                keys: revoked ? [] : [{ ...metadata, id: 2 }],
                next_cursor: 0,
                total: revoked ? 1 : 51,
                page: 2,
                page_size: 50,
              },
        ),
      )
    }),
  )
  const user = userEvent.setup()
  open()
  await screen.findByText('Synthetic first')
  await user.click(screen.getByRole('button', { name: 'Next' }))
  await user.click(
    await screen.findByRole('button', { name: 'Revoke Synthetic laptop' }),
  )
  const dialog = await screen.findByRole('dialog', { name: 'Revoke API key' })
  await user.click(within(dialog).getByRole('button', { name: 'Revoke key' }))
  await screen.findByText('Synthetic laptop was revoked.')
  await screen.findByText('Synthetic first')
  expect(screen.queryByText('No unrevoked API keys')).toBeNull()
  expect(
    (screen.getByRole('button', { name: 'Previous' }) as HTMLButtonElement)
      .disabled,
  ).toBe(true)
})

it('lets a member create, copy once, and revoke an owned key', async () => {
  let keys: (typeof metadata)[] = []
  const fetchMock = vi
    .fn()
    .mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(memberAuthenticated))
      if (url === '/api/keys/groups')
        return Promise.resolve(
          response({ groups: [{ id: 1, name: 'Default', account_count: 1 }] }),
        )
      if (url.endsWith('/revoke')) {
        keys = [{ ...metadata, revoked_at: 1900000100, copyable: false }]
        return Promise.resolve(response(keys[0]))
      }
      if (init?.method === 'POST') {
        keys = [metadata]
        return Promise.resolve(response({ key: metadata, secret }))
      }
      const includeRevoked =
        new URL(url, 'http://example.test').searchParams.get(
          'include_revoked',
        ) === 'true'
      return Promise.resolve(
        response({
          keys: keys.filter((key) => includeRevoked || key.revoked_at === null),
          next_cursor: 0,
        }),
      )
    })
  vi.stubGlobal('fetch', fetchMock)
  const user = userEvent.setup()
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  open()
  await screen.findByText('No unrevoked API keys')
  await user.click(screen.getByRole('button', { name: 'Create key' }))
  const form = await screen.findByRole('dialog', { name: 'Create API key' })
  await user.click(within(form).getByRole('button', { name: 'Resource group' }))
  await user.click(
    await screen.findByRole('menuitemradio', { name: /Default/ }),
  )
  await user.type(within(form).getByLabelText('Name'), 'Synthetic laptop')
  await user.click(within(form).getByRole('button', { name: 'Create key' }))
  const created = await screen.findByRole('dialog', {
    name: 'Save your API key',
  })
  expect(
    (within(created).getByLabelText('API key') as HTMLInputElement).value,
  ).toBe(secret)
  await user.click(within(created).getByRole('button', { name: 'Copy key' }))
  await waitFor(() => expect(copy).toHaveBeenCalledWith(secret))
  await user.click(within(created).getByRole('button', { name: 'Done' }))
  expect(screen.queryByDisplayValue(secret)).toBeNull()
  await user.click(
    await screen.findByRole('button', { name: 'Revoke Synthetic laptop' }),
  )
  const revoke = await screen.findByRole('dialog', { name: 'Revoke API key' })
  await user.click(within(revoke).getByRole('button', { name: 'Revoke key' }))
  expect(await screen.findByText('Synthetic laptop was revoked.')).toBeTruthy()
  await screen.findByText('No unrevoked API keys')
  expect(screen.queryByText('Synthetic laptop')).toBeNull()
  await user.click(screen.getByRole('switch', { name: 'Show revoked' }))
  expect(await screen.findByText('Revoked')).toBeTruthy()
  expect(
    fetchMock.mock.calls.some(
      ([url]) => url === '/api/system' || url.startsWith('/api/members'),
    ),
  ).toBe(false)
})

it('discards a displayed secret when a background check changes the user', async () => {
  const client = queries.createQueryClient()
  vi.spyOn(queries, 'createQueryClient').mockReturnValue(client)
  let state = memberAuthenticated
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/auth/state') return Promise.resolve(response(state))
      if (url === '/api/keys/groups')
        return Promise.resolve(
          response({ groups: [{ id: 1, name: 'Default', account_count: 1 }] }),
        )
      if (init?.method === 'POST')
        return Promise.resolve(response({ key: metadata, secret }))
      return Promise.resolve(response({ keys: [], next_cursor: 0 }))
    }),
  )
  const user = userEvent.setup()
  open()
  await screen.findByText('No unrevoked API keys')
  await user.click(screen.getByRole('button', { name: 'Create key' }))
  const form = await screen.findByRole('dialog', { name: 'Create API key' })
  await user.click(within(form).getByRole('button', { name: 'Resource group' }))
  await user.click(
    await screen.findByRole('menuitemradio', { name: /Default/ }),
  )
  await user.type(within(form).getByLabelText('Name'), 'Synthetic laptop')
  await user.click(within(form).getByRole('button', { name: 'Create key' }))
  await screen.findByDisplayValue(secret)
  state = {
    ...memberAuthenticated,
    user: { ...memberAuthenticated.user, id: 3, username: 'another-test' },
  }
  await act(async () => {
    await client.fetchQuery({ ...authOptions(), staleTime: 0 })
  })
  await waitFor(() => expect(screen.queryByDisplayValue(secret)).toBeNull())
  expect(screen.queryByRole('dialog')).toBeNull()
  await waitFor(() =>
    expect(client.getMutationCache().getAll()).toHaveLength(0),
  )
  client.clear()
})

it('requires an explicit pool choice before binding a key', async () => {
  const fetch = vi
    .fn()
    .mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(memberAuthenticated))
      if (url === '/api/keys/groups')
        return Promise.resolve(
          response({
            groups: [
              { id: 2, name: 'Project alpha', account_count: 1 },
              { id: 3, name: 'Project beta', account_count: 1 },
            ],
          }),
        )
      if (url === '/api/keys' && init?.method === 'POST') {
        expect(JSON.parse(String(init.body))).toEqual({
          name: 'Synthetic project key',
          group_id: 3,
          expires_at: null,
        })
        return Promise.resolve(
          response({
            key: { ...metadata, group_id: 3, group_name: 'Project beta' },
            secret,
          }),
        )
      }
      if (url === '/api/connection')
        return Promise.resolve(response({ status: 'ready' }))
      return Promise.resolve(response({ keys: [], next_cursor: 0 }))
    })
  vi.stubGlobal('fetch', fetch)
  open()
  const user = userEvent.setup()
  await screen.findByText('No unrevoked API keys')
  await user.click(screen.getByRole('button', { name: 'Create key' }))
  const dialog = await screen.findByRole('dialog', { name: 'Create API key' })
  await within(dialog).findByText('Choose a resource group')
  expect(
    within(dialog)
      .getByRole('button', { name: 'Create key' })
      .hasAttribute('disabled'),
  ).toBe(true)
  await user.click(
    within(dialog).getByRole('button', { name: 'Resource group' }),
  )
  await user.click(screen.getByRole('menuitemradio', { name: /Project beta/ }))
  await user.type(
    within(dialog).getByLabelText('Name'),
    'Synthetic project key',
  )
  await user.click(within(dialog).getByRole('button', { name: 'Create key' }))
  await screen.findByDisplayValue(secret)
})

it('warns that a key bound to an empty pool cannot serve requests yet', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(memberAuthenticated))
      if (url === '/api/keys/groups')
        return Promise.resolve(
          response({
            groups: [{ id: 9, name: 'Synthetic empty pool', account_count: 0 }],
          }),
        )
      return Promise.resolve(response({ keys: [], next_cursor: 0 }))
    }),
  )
  const user = userEvent.setup()
  open()
  await screen.findByText('No unrevoked API keys')
  await user.click(screen.getByRole('button', { name: 'Create key' }))
  const dialog = await screen.findByRole('dialog', { name: 'Create API key' })
  await user.click(
    within(dialog).getByRole('button', { name: 'Resource group' }),
  )
  await user.click(
    await screen.findByRole('menuitemradio', { name: /Synthetic empty pool/ }),
  )
  expect(
    within(dialog).getByText(
      'This group has no resources. A new key cannot serve requests until a resource is added.',
    ),
  ).toBeTruthy()
})

it('opens client setup on demand, copies the model config, and restores focus on close', async () => {
  const fetch = vi.fn().mockImplementation((url: string) => {
    if (url === '/api/auth/state')
      return Promise.resolve(response(memberAuthenticated))
    if (url === '/api/connection')
      return Promise.resolve(response({ status: 'ready' }))
    return Promise.resolve(response({ keys: [], next_cursor: 0 }))
  })
  vi.stubGlobal('fetch', fetch)
  const user = userEvent.setup()
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  open()
  await screen.findByText('No unrevoked API keys')
  expect(fetch.mock.calls.some(([url]) => url === '/api/connection')).toBe(
    false,
  )
  const trigger = screen.getByRole('button', { name: 'Setup guide' })
  await user.click(trigger)
  const guide = await screen.findByRole('dialog', {
    name: 'Connect your client',
  })
  await within(guide).findByText('Ready')
  await user.type(
    within(guide).getByLabelText('Model ID'),
    'codex/synthetic-model',
  )
  await user.click(
    within(guide).getByRole('button', { name: 'Copy configuration' }),
  )
  await waitFor(() =>
    expect(copy).toHaveBeenCalledWith(
      expect.stringContaining('model = "codex/synthetic-model"'),
    ),
  )
  expect(copy.mock.calls[0][0]).toContain('env_key = "SUBLANE_API_KEY"')
  await user.keyboard('{Escape}')
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  expect(document.activeElement).toBe(trigger)
  await user.click(trigger)
  expect(
    ((await screen.findByLabelText('Model ID')) as HTMLInputElement).value,
  ).toBe('codex/synthetic-model')
})

it('switches client protocols and copies matching request examples', async () => {
  const fetch = vi.fn().mockImplementation((url: string) => {
    if (url === '/api/auth/state')
      return Promise.resolve(response(memberAuthenticated))
    if (url === '/api/connection')
      return Promise.resolve(response({ status: 'ready' }))
    return Promise.resolve(response({ keys: [], next_cursor: 0 }))
  })
  vi.stubGlobal('fetch', fetch)
  const user = userEvent.setup()
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  open()
  await user.click(await screen.findByRole('button', { name: 'Setup guide' }))
  const guide = await screen.findByRole('dialog', {
    name: 'Connect your client',
  })
  await user.type(within(guide).getByLabelText('Model ID'), 'synthetic-model')
  for (const [label, endpoint, header, basePath] of [
    [
      'Cline / OpenAI compatible',
      '/v1/chat/completions',
      'Authorization: Bearer',
      '/v1',
    ],
    ['Claude Messages', '/v1/messages', 'x-api-key', ''],
    [
      'Gemini API',
      '/v1beta/models/synthetic-model:generateContent',
      'x-goog-api-key',
      '',
    ],
  ]) {
    await user.click(
      within(guide).getByRole('button', { name: 'Client protocol' }),
    )
    await user.click(await screen.findByRole('menuitemradio', { name: label }))
    expect(
      (within(guide).getByLabelText('API base URL') as HTMLInputElement).value,
    ).toBe(window.location.origin + basePath)
    if (label === 'Cline / OpenAI compatible') {
      expect(
        within(guide).getByText(/In Cline settings, select OpenAI Compatible/),
      ).toBeTruthy()
    }
    await user.click(
      within(guide).getByRole('button', { name: 'Copy configuration' }),
    )
    await waitFor(() =>
      expect(copy).toHaveBeenLastCalledWith(expect.stringContaining(endpoint)),
    )
    expect(copy.mock.calls.at(-1)?.[0]).toContain(header)
    expect(copy.mock.calls.at(-1)?.[0]).toContain('$SUBLANE_API_KEY')
  }
  expect(
    fetch.mock.calls.some(([url]) => String(url).endsWith('/secret')),
  ).toBe(false)
})

it('renames and pauses a key without replacing its secret or group', async () => {
  let value = { ...metadata, enabled: true, expires_at: null }
  const fetch = vi
    .fn()
    .mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(memberAuthenticated))
      if (init?.method === 'PATCH') {
        expect(JSON.parse(String(init.body))).toEqual({
          name: 'Paused laptop',
          enabled: false,
          expires_at: null,
        })
        value = { ...value, ...JSON.parse(String(init.body)) }
        return Promise.resolve(response(value))
      }
      return Promise.resolve(
        response({ keys: [value], next_cursor: 0, server_time: 1900000000 }),
      )
    })
  vi.stubGlobal('fetch', fetch)
  const user = userEvent.setup()
  open()
  await user.click(
    await screen.findByRole('button', { name: 'Edit Synthetic laptop' }),
  )
  const dialog = await screen.findByRole('dialog', { name: 'Edit API key' })
  await user.clear(within(dialog).getByLabelText('Name'))
  await user.type(within(dialog).getByLabelText('Name'), 'Paused laptop')
  await user.click(within(dialog).getByLabelText('Key enabled'))
  await user.click(within(dialog).getByRole('button', { name: 'Save changes' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  expect(await screen.findByText('Paused')).toBeTruthy()
  expect(screen.queryByDisplayValue(secret)).toBeNull()
})

it('uses the server clock to display expired keys', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string) =>
      Promise.resolve(
        response(
          url === '/api/auth/state'
            ? memberAuthenticated
            : {
                keys: [{ ...metadata, expires_at: 1899999999 }],
                next_cursor: 0,
                server_time: 1900000000,
              },
        ),
      ),
    ),
  )
  open()
  expect(await screen.findByText('Expired')).toBeTruthy()
})
it('creates a key with the selected expiry period', async () => {
  const before = Math.floor(Date.now() / 1000)
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(memberAuthenticated))
      if (url === '/api/keys/groups')
        return Promise.resolve(
          response({ groups: [{ id: 1, name: 'Default', account_count: 1 }] }),
        )
      if (init?.method === 'POST') {
        const data = JSON.parse(String(init.body))
        expect(data.expires_at).toBeGreaterThanOrEqual(before + 7 * 86400)
        expect(data.expires_at).toBeLessThanOrEqual(
          Math.floor(Date.now() / 1000) + 7 * 86400,
        )
        return Promise.resolve(
          response({
            key: { ...metadata, expires_at: data.expires_at },
            secret,
          }),
        )
      }
      return Promise.resolve(response({ keys: [], next_cursor: 0 }))
    }),
  )
  const user = userEvent.setup()
  open()
  await screen.findByText('No unrevoked API keys')
  await user.click(screen.getByRole('button', { name: 'Create key' }))
  const dialog = await screen.findByRole('dialog')
  await user.click(
    within(dialog).getByRole('button', { name: 'Resource group' }),
  )
  await user.click(
    await screen.findByRole('menuitemradio', { name: /Default/ }),
  )
  await user.type(
    within(dialog).getByLabelText('Name'),
    'Synthetic expiring key',
  )
  await user.click(within(dialog).getByRole('button', { name: 'Expires' }))
  await user.click(screen.getByRole('menuitemradio', { name: 'In 7 days' }))
  await user.click(within(dialog).getByRole('button', { name: 'Create key' }))
  await screen.findByDisplayValue(secret)
})

it('fetches a full key only for explicit copy and never retains it in query or mutation data', async () => {
  const client = queries.createQueryClient()
  vi.spyOn(queries, 'createQueryClient').mockReturnValue(client)
  const fetch = vi
    .fn()
    .mockImplementation((url: string) =>
      Promise.resolve(
        response(
          url === '/api/auth/state'
            ? memberAuthenticated
            : url === '/api/keys/1/secret'
              ? { secret }
              : { keys: [{ ...metadata, copyable: true }], next_cursor: 0 },
        ),
      ),
    )
  vi.stubGlobal('fetch', fetch)
  const user = userEvent.setup()
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  open()
  const button = await screen.findByRole('button', {
    name: 'Copy Synthetic laptop',
  })
  expect(fetch.mock.calls.some(([url]) => url === '/api/keys/1/secret')).toBe(
    false,
  )
  await user.click(button)
  await waitFor(() => expect(copy).toHaveBeenCalledWith(secret))
  expect(screen.queryByDisplayValue(secret)).toBeNull()
  await user.click(button)
  await waitFor(() => expect(copy).toHaveBeenCalledTimes(2))
  expect(
    fetch.mock.calls.filter(([url]) => url === '/api/keys/1/secret'),
  ).toHaveLength(2)
  expect(
    JSON.stringify(
      client
        .getQueryCache()
        .getAll()
        .map((query) => query.state.data),
    ),
  ).not.toContain(secret)
  expect(
    JSON.stringify(
      client
        .getMutationCache()
        .getAll()
        .map((mutation) => mutation.state.data),
    ),
  ).not.toContain(secret)
})
it('explains legacy keys and offers manual copy when clipboard access fails', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string) =>
      Promise.resolve(
        response(
          url === '/api/auth/state'
            ? memberAuthenticated
            : url === '/api/keys/1/secret'
              ? { secret }
              : {
                  keys: [
                    { ...metadata, copyable: true },
                    {
                      ...metadata,
                      id: 2,
                      name: 'Synthetic legacy key',
                      copyable: false,
                    },
                  ],
                  next_cursor: 0,
                },
        ),
      ),
    ),
  )
  const user = userEvent.setup()
  vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(
    new Error('synthetic clipboard denial'),
  )
  open()
  const disabled = await screen.findByRole('button', {
    name: 'Copy Synthetic legacy key',
  })
  expect(disabled.hasAttribute('disabled')).toBe(true)
  expect(
    screen.getByText(
      'This older key cannot be copied again. Create a new key if you need its full value.',
    ),
  ).toBeTruthy()
  await user.click(
    screen.getByRole('button', { name: 'Copy Synthetic laptop' }),
  )
  const dialog = await screen.findByRole('dialog', { name: 'Copy API key' })
  expect(
    (within(dialog).getByLabelText('API key') as HTMLInputElement).value,
  ).toBe(secret)
  await user.click(within(dialog).getByRole('button', { name: 'Done' }))
  await waitFor(() => expect(screen.queryByDisplayValue(secret)).toBeNull())
  expect(document.activeElement).toBe(
    screen.getByRole('button', { name: 'Copy Synthetic laptop' }),
  )
})
it.each([200, 401])(
  'ignores a late secret response (%i) after an identity change',
  async (status) => {
    const client = queries.createQueryClient()
    vi.spyOn(queries, 'createQueryClient').mockReturnValue(client)
    let state = memberAuthenticated
    let resolveSecret!: (value: Response) => void
    let signal: AbortSignal | undefined
    const pending = new Promise<Response>((resolve) => {
      resolveSecret = resolve
    })
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((url: string, init?: RequestInit) => {
        if (url === '/api/auth/state') return Promise.resolve(response(state))
        if (url === '/api/keys/1/secret') {
          signal = init?.signal ?? undefined
          return pending
        }
        return Promise.resolve(
          response({ keys: [{ ...metadata, copyable: true }], next_cursor: 0 }),
        )
      }),
    )
    const user = userEvent.setup()
    const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
    open()
    await user.click(
      await screen.findByRole('button', { name: 'Copy Synthetic laptop' }),
    )
    state = {
      ...memberAuthenticated,
      user: {
        ...memberAuthenticated.user,
        id: 3,
        username: 'other-synthetic-member',
      },
    }
    await act(async () => {
      await client.fetchQuery({ ...authOptions(), staleTime: 0 })
    })
    await waitFor(() => expect(signal?.aborted).toBe(true))
    await act(async () => {
      resolveSecret(
        status === 200
          ? response({ secret })
          : new Response(JSON.stringify({ error: 'unauthorized' }), { status }),
      )
      await pending
    })
    expect(copy).not.toHaveBeenCalled()
    expect(
      client.getQueryData<{ user: { id: number } }>(['auth'])?.user?.id,
    ).toBe(3)
    expect(screen.queryByDisplayValue(secret)).toBeNull()
  },
)
