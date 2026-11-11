import {
  fireEvent,
  render,
  screen,
  within,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryHistory } from '@tanstack/react-router'
import { expect, it, vi } from 'vitest'
import { App } from '@/App'
import { createAppRouter } from '@/router'
import * as queries from '@/lib/query'
import { authenticated } from '@/test/fixtures'

const channel = {
  id: 'synthetic-channel',
  name: 'Synthetic API',
  protocol: 'openai',
  base_url: 'https://relay.example.test/v1',
  enabled: true,
  status: 'unverified',
  max_concurrency: 30,
  created_at: 1,
  updated_at: 1,
}
const response = (value: unknown) => new Response(JSON.stringify(value))

it('explains when an older backend does not provide channel management', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(authenticated))
      if (url === '/api/channels')
        return Promise.resolve(
          new Response(JSON.stringify({ error: 'not_found' }), { status: 404 }),
        )
      if (url === '/api/channels/runtime')
        return Promise.resolve(
          new Response(JSON.stringify({ error: 'not_found' }), { status: 404 }),
        )
      return Promise.resolve(response({}))
    }),
  )
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/channels'] }),
      )}
    />,
  )
  await screen.findByText(
    'This backend does not support API channels. Start an updated backend and reconnect.',
  )
  expect(screen.queryByText('No API channels')).toBeNull()
})

it.each([
  { status: 404, error: 'workspace_not_found' },
  { status: 503, error: 'unavailable' },
])('keeps a generic load error for $error', async ({ status, error }) => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(authenticated))
      if (url === '/api/channels')
        return Promise.resolve(
          new Response(JSON.stringify({ error }), { status }),
        )
      if (url === '/api/channels/runtime')
        return Promise.resolve(response({ channels: [], server_time: 1 }))
      return Promise.resolve(response({}))
    }),
  )
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/channels'] }),
      )}
    />,
  )
  await screen.findByText('Could not load API channels.')
  expect(
    screen.queryByText(
      'This backend does not support API channels. Start an updated backend and reconnect.',
    ),
  ).toBeNull()
})

it('creates a channel independently and removes the key from mutation state', async () => {
  let saved = false
  const fetch = vi
    .fn()
    .mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(authenticated))
      if (url === '/api/proxies')
        return Promise.resolve(response({ proxies: [] }))
      if (url === '/api/channels' && init?.method === 'POST') {
        expect(JSON.parse(String(init.body))).toEqual({
          name: 'Synthetic API',
          api_key: 'synthetic-key',
          base_url: 'https://relay.example.test/v1',
          platform: 'openai',
          protocol: 'openai',
        })
        saved = true
        return Promise.resolve(response(channel))
      }
      if (url === '/api/channels')
        return Promise.resolve(response({ channels: saved ? [channel] : [] }))
      if (url === '/api/channels/runtime')
        return Promise.resolve(response({ channels: [], server_time: 1 }))
      return Promise.resolve(response({}))
    })
  vi.stubGlobal('fetch', fetch)
  const client = queries.createQueryClient()
  vi.spyOn(queries, 'createQueryClient').mockReturnValue(client)
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/channels'] }),
      )}
    />,
  )
  const user = userEvent.setup()
  await screen.findByRole('heading', { name: 'API channels' })
  await user.click(screen.getByRole('link', { name: 'Add API channel' }))
  await screen.findByRole('heading', { name: 'Add API channel' })
  expect(
    screen
      .getByRole('link', { name: 'API channels' })
      .getAttribute('aria-current'),
  ).toBe('page')
  expect(
    screen
      .getByRole('link', { name: 'API channels' })
      .getAttribute('data-active'),
  ).toBe('true')
  expect(screen.queryByRole('dialog')).toBeNull()
  const dialog = screen.getByRole('main')
  await user.type(
    within(dialog).getByLabelText('Channel name'),
    'Synthetic API',
  )
  await user.clear(within(dialog).getByLabelText('Base URL'))
  await user.type(
    within(dialog).getByLabelText('Base URL'),
    'https://relay.example.test/v1',
  )
  const key = within(dialog).getByLabelText('Upstream API key')
  expect(key.getAttribute('type')).toBe('password')
  await user.type(key, 'synthetic-key')
  await user.click(within(dialog).getByRole('button', { name: 'Connect API' }))
  await screen.findByText('Synthetic API')
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  await waitFor(() =>
    expect(client.getMutationCache().getAll()).toHaveLength(0),
  )
  expect(
    fetch.mock.calls.some(([url]) => String(url).startsWith('/api/accounts')),
  ).toBe(false)
})

it('renames a channel, keeps its stored key and locks its endpoint', async () => {
  const bodies: unknown[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(authenticated))
      if (url === '/api/channels')
        return Promise.resolve(response({ channels: [channel] }))
      if (url === '/api/channels/runtime')
        return Promise.resolve(response({ channels: [], server_time: 1 }))
      if (url === '/api/channels/synthetic-channel' && init?.method === 'PUT') {
        bodies.push(JSON.parse(String(init.body)))
        return Promise.resolve(response(channel))
      }
      return Promise.resolve(response({}))
    }),
  )
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/channels'] }),
      )}
    />,
  )
  const user = userEvent.setup()
  await screen.findByText('Synthetic API')
  await user.click(
    screen.getByRole('button', { name: 'Actions for Synthetic API' }),
  )
  await user.click(screen.getByRole('menuitem', { name: 'Edit channel' }))
  const dialog = await screen.findByRole('dialog')
  const key = within(dialog).getByLabelText(
    'Upstream API key',
  ) as HTMLInputElement
  expect(key.value).toBe('')
  const name = within(dialog).getByLabelText('Channel name')
  await user.clear(name)
  await user.type(name, 'Synthetic renamed')
  expect(
    (within(dialog).getByLabelText('Base URL') as HTMLInputElement).readOnly,
  ).toBe(true)
  await user.click(within(dialog).getByRole('button', { name: 'Save channel' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  expect(bodies).toEqual([
    {
      name: 'Synthetic renamed',
      base_url: channel.base_url,
      platform: 'custom',
      protocol: 'openai',
    },
  ])
})

it('selects platform presets and accepts a custom endpoint and protocol', async () => {
  let submitted: Record<string, string> | undefined
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(authenticated))
      if (url === '/api/proxies')
        return Promise.resolve(response({ proxies: [] }))
      if (url === '/api/channels/runtime')
        return Promise.resolve(response({ channels: [], server_time: 1 }))
      if (url === '/api/channels' && init?.method === 'POST') {
        submitted = JSON.parse(String(init.body))
        return Promise.resolve(response({ ...channel, ...submitted }))
      }
      if (url === '/api/channels')
        return Promise.resolve(response({ channels: [] }))
      return Promise.resolve(response({}))
    }),
  )
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/channels'] }),
      )}
    />,
  )
  const user = userEvent.setup()
  await user.click(await screen.findByRole('link', { name: 'Add API channel' }))
  await screen.findByRole('heading', { name: 'Add API channel' })
  expect(screen.queryByRole('dialog')).toBeNull()
  const dialog = screen.getByRole('main')
  await user.click(within(dialog).getByLabelText('Platform'))
  await user.click(await screen.findByRole('menuitemradio', { name: 'Gemini' }))
  expect(
    (within(dialog).getByLabelText('Base URL') as HTMLInputElement).value,
  ).toBe('https://generativelanguage.googleapis.com/v1beta')
  expect(
    within(dialog).getByLabelText('Upstream protocol').textContent,
  ).toContain('Gemini')
  await user.click(within(dialog).getByLabelText('Platform'))
  await user.click(await screen.findByRole('menuitemradio', { name: 'Custom' }))
  await user.click(within(dialog).getByLabelText('Upstream protocol'))
  await user.click(
    await screen.findByRole('menuitemradio', { name: 'OpenAI Responses' }),
  )
  await user.type(
    within(dialog).getByLabelText('Channel name'),
    'Synthetic custom',
  )
  await user.clear(within(dialog).getByLabelText('Base URL'))
  await user.type(
    within(dialog).getByLabelText('Base URL'),
    'https://custom.example.test/v1',
  )
  await user.type(
    within(dialog).getByLabelText('Upstream API key'),
    'synthetic-key',
  )
  await user.click(within(dialog).getByRole('button', { name: 'Connect API' }))
  await waitFor(() =>
    expect(submitted).toEqual({
      name: 'Synthetic custom',
      api_key: 'synthetic-key',
      base_url: 'https://custom.example.test/v1',
      platform: 'custom',
      protocol: 'responses',
    }),
  )
})

it('searches platform and protocol choices without submitting the channel form', async () => {
  const fetch = vi
    .fn()
    .mockImplementation((url: string, init?: RequestInit) => {
      if (init?.method === 'POST')
        return Promise.reject(new Error('Unexpected mutation'))
      if (url === '/api/auth/state')
        return Promise.resolve(response(authenticated))
      if (url === '/api/proxies')
        return Promise.resolve(response({ proxies: [] }))
      return Promise.resolve(response({}))
    })
  vi.stubGlobal('fetch', fetch)
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/channels/new'] }),
      )}
    />,
  )
  const user = userEvent.setup()
  const platform = await screen.findByRole('button', {
    name: 'Platform: OpenAI',
  })
  await user.click(platform)
  let search = await screen.findByRole('searchbox', {
    name: 'Search platforms',
  })
  expect(document.activeElement).toBe(search)
  await user.type(search, '  gEmI ')
  expect(screen.getAllByRole('menuitemradio')).toHaveLength(1)
  fireEvent.keyDown(search, { key: 'Enter', isComposing: true })
  expect(screen.getByRole('menu')).toBeTruthy()
  await user.keyboard('{ArrowDown}{Enter}')
  await waitFor(() => expect(platform.textContent).toContain('Gemini'))
  expect((screen.getByLabelText('Base URL') as HTMLInputElement).value).toBe(
    'https://generativelanguage.googleapis.com/v1beta',
  )
  await user.click(platform)
  search = await screen.findByRole('searchbox', { name: 'Search platforms' })
  expect((search as HTMLInputElement).value).toBe('')
  await user.type(search, 'generativelanguage.googleapis.com')
  expect(screen.getAllByRole('menuitemradio')).toHaveLength(1)
  await user.clear(search)
  await user.type(search, 'synthetic-no-such-platform')
  expect(screen.queryAllByRole('menuitemradio')).toHaveLength(0)
  await screen.findByText('No matching platforms.')
  await user.keyboard('{Enter}')
  expect(screen.getByRole('menu')).toBeTruthy()
  await user.keyboard('{Escape}')
  await waitFor(() => expect(screen.queryByRole('menu')).toBeNull())
  expect(document.activeElement).toBe(platform)
  await user.click(platform)
  search = await screen.findByRole('searchbox', { name: 'Search platforms' })
  await user.type(search, 'custom')
  await user.keyboard('{Enter}')
  await user.click(
    await screen.findByRole('button', { name: 'Upstream protocol: Gemini' }),
  )
  await user.type(
    await screen.findByRole('searchbox', { name: 'Search protocols' }),
    'responses',
  )
  expect(screen.getAllByRole('menuitemradio')).toHaveLength(1)
  await user.keyboard('{ArrowUp}{Enter}')
  await screen.findByRole('button', {
    name: 'Upstream protocol: OpenAI Responses',
  })
  expect(screen.queryByRole('alert')).toBeNull()
  expect(
    fetch.mock.calls.some(
      ([, init]) => (init as RequestInit | undefined)?.method === 'POST',
    ),
  ).toBe(false)
})
