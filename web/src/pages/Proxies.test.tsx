import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryHistory } from '@tanstack/react-router'
import { expect, it, vi } from 'vitest'
import { App } from '@/App'
import { createAppRouter } from '@/router'
import { authenticated } from '@/test/fixtures'

const response = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), { status })

it('creates a proxy without displaying its credentials', async () => {
  const proxies: unknown[] = []
  const fetch = vi
    .fn()
    .mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(authenticated))
      if (url === '/api/proxies' && init?.method === 'POST') {
        expect(JSON.parse(String(init.body))).toEqual({
          name: 'Synthetic exit',
          url: 'http://synthetic-user:synthetic-pass@127.0.0.1:18080',
        })
        const proxy = {
          id: 'synthetic-proxy',
          name: 'Synthetic exit',
          endpoint: 'http://127.0.0.1:18080',
          account_count: 0,
          created_at: 1,
          updated_at: 1,
        }
        proxies.push(proxy)
        return Promise.resolve(response(proxy, 201))
      }
      if (url === '/api/proxies') return Promise.resolve(response({ proxies }))
      return Promise.resolve(response({}))
    })
  vi.stubGlobal('fetch', fetch)
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/admin/proxies'] }),
      )}
    />,
  )
  const user = userEvent.setup()
  await screen.findByRole('heading', { name: 'Network proxies' })
  await user.click(screen.getByRole('button', { name: 'Add proxy' }))
  const dialog = await screen.findByRole('dialog')
  await user.type(within(dialog).getByLabelText('Proxy name'), 'Synthetic exit')
  await user.type(
    within(dialog).getByLabelText('Proxy URL'),
    'http://synthetic-user:synthetic-pass@127.0.0.1:18080',
  )
  await user.click(within(dialog).getByRole('button', { name: 'Save proxy' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  await screen.findByText('Synthetic exit')
  expect(screen.queryByText(/synthetic-pass/)).toBeNull()
  for (const name of [
    'Test Synthetic exit',
    'Edit Synthetic exit',
    'Delete Synthetic exit',
  ]) {
    const button = screen.getByRole('button', { name })
    expect(button.getAttribute('aria-label')).toBe(name)
    expect(button.textContent?.trim()).toBe('')
  }
})

it('tests every proxy even when one check request fails', async () => {
  const proxies = [
    {
      id: 'proxy-one',
      name: 'First',
      endpoint: 'http://127.0.0.1:18080',
      account_count: 0,
      created_at: 1,
      updated_at: 1,
    },
    {
      id: 'proxy-two',
      name: 'Second',
      endpoint: 'http://127.0.0.1:18081',
      account_count: 0,
      created_at: 1,
      updated_at: 1,
    },
    {
      id: 'proxy-three',
      name: 'Third',
      endpoint: 'http://127.0.0.1:18082',
      account_count: 0,
      created_at: 1,
      updated_at: 1,
    },
  ]
  const calls: string[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(authenticated))
      if (url === '/api/proxies') return Promise.resolve(response({ proxies }))
      if (url.endsWith('/check') && init?.method === 'POST') {
        calls.push(url)
        if (url.includes('proxy-two'))
          return Promise.resolve(response({ error: 'unavailable' }, 503))
        return Promise.resolve(
          response({
            ...proxies.find((proxy) => url.includes(proxy.id)),
            checked_at: 1900000000,
            reachable: true,
            exit_ip: '203.0.113.8',
            country: 'US',
            city: 'Test city',
          }),
        )
      }
      return Promise.reject(new Error('Unexpected request'))
    }),
  )
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/admin/proxies'] }),
      )}
    />,
  )
  await screen.findByRole('heading', { name: 'Network proxies' })
  await userEvent
    .setup()
    .click(screen.getByRole('button', { name: 'Test all proxies' }))
  await screen.findByText('3 proxies checked')
  expect(calls).toHaveLength(3)
})

it('confirms bulk deletion of recently failed unbound proxies', async () => {
  const now = Math.floor(Date.now() / 1000)
  let proxies = [
    {
      id: 'failed',
      name: 'Failed',
      endpoint: 'http://127.0.0.1:18080',
      account_count: 0,
      created_at: 1,
      updated_at: 1,
      checked_at: now,
      reachable: false,
      check_error: 'connection_failed',
      prune_eligible: true,
    },
    {
      id: 'bound',
      name: 'Bound',
      endpoint: 'http://127.0.0.1:18081',
      account_count: 1,
      created_at: 1,
      updated_at: 1,
      checked_at: now,
      reachable: false,
      check_error: 'connection_failed',
      prune_eligible: false,
    },
    {
      id: 'unknown',
      name: 'Unknown',
      endpoint: 'http://127.0.0.1:18082',
      account_count: 0,
      created_at: 1,
      updated_at: 1,
    },
  ]
  const fetch = vi
    .fn()
    .mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(authenticated))
      if (url === '/api/proxies') return Promise.resolve(response({ proxies }))
      if (url === '/api/proxies/prune' && init?.method === 'POST') {
        proxies = proxies.filter((proxy) => proxy.id !== 'failed')
        return Promise.resolve(response({ deleted_count: 1 }))
      }
      return Promise.reject(new Error('Unexpected request'))
    })
  vi.stubGlobal('fetch', fetch)
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/admin/proxies'] }),
      )}
    />,
  )
  const user = userEvent.setup()
  await screen.findByRole('heading', { name: 'Network proxies' })
  await screen.findByRole('heading', { name: 'Failed' })
  await user.click(
    screen.getByRole('button', { name: 'Delete failed proxies (1)' }),
  )
  const dialog = await screen.findByRole('dialog')
  await user.click(
    within(dialog).getByRole('button', { name: 'Delete 1 failed proxy' }),
  )
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  expect(screen.queryByRole('heading', { name: 'Failed' })).toBeNull()
  expect(screen.getByRole('heading', { name: 'Bound' })).toBeTruthy()
})

it('imports multiple proxies and displays a checked exit location', async () => {
  let proxies: Record<string, unknown>[] = []
  const fetch = vi
    .fn()
    .mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(authenticated))
      if (url === '/api/proxies/import' && init?.method === 'POST') {
        expect(JSON.parse(String(init.body))).toEqual({
          text: 'http://127.0.0.1:18080\nNamed exit | socks5://127.0.0.1:19090',
        })
        proxies = [
          {
            id: 'proxy-one',
            name: '127.0.0.1:18080',
            endpoint: 'http://127.0.0.1:18080',
            account_count: 0,
            created_at: 1,
            updated_at: 1,
          },
          {
            id: 'proxy-two',
            name: 'Named exit',
            endpoint: 'socks5://127.0.0.1:19090',
            account_count: 0,
            created_at: 1,
            updated_at: 1,
          },
        ]
        return Promise.resolve(response({ proxies }, 201))
      }
      if (url === '/api/proxies/proxy-one/check' && init?.method === 'POST') {
        proxies[0] = {
          ...proxies[0],
          checked_at: 1900000000,
          reachable: true,
          exit_ip: '203.0.113.8',
          country: 'US',
          region: 'Test region',
          city: 'Test city',
          latency_ms: 42,
          check_error: '',
        }
        return Promise.resolve(response(proxies[0]))
      }
      // Reads only return cached reports; the explicit refresh is the one disclosure.
      if (url === '/api/proxies/proxy-one/ip-report')
        return Promise.resolve(response({ error: 'ip_report_not_loaded' }, 404))
      if (
        url === '/api/proxies/proxy-one/ip-report/refresh' &&
        init?.method === 'POST'
      )
        return Promise.resolve(
          response({
            ip: '203.0.113.8',
            trust_score: 86,
            company_type: 'isp',
            residential: true,
            datacenter: false,
            mobile: false,
            vpn: false,
            proxy: false,
            tor: false,
            abuser: false,
            crawler: false,
            asn: 64500,
            as_organization: 'Synthetic ISP',
            isp: 'Synthetic ISP',
            country_code: 'US',
            country: 'United States',
            region: 'Test region',
            city: 'Test city',
            verdict: '',
          }),
        )
      if (url === '/api/proxies') return Promise.resolve(response({ proxies }))
      return Promise.reject(new Error('Unexpected request'))
    })
  vi.stubGlobal('fetch', fetch)
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/admin/proxies'] }),
      )}
    />,
  )
  const user = userEvent.setup()
  await screen.findByRole('heading', { name: 'Network proxies' })
  // Unchecked proxies explain how to obtain a report instead of calling the reputation service.
  expect(
    fetch.mock.calls.some(([url]) => String(url).includes('ip-report')),
  ).toBe(false)
  await user.click(screen.getByRole('button', { name: 'Import proxies' }))
  const dialog = await screen.findByRole('dialog')
  await user.type(
    within(dialog).getByLabelText('Proxy list'),
    'http://127.0.0.1:18080\nNamed exit | socks5://127.0.0.1:19090',
  )
  await user.click(
    within(dialog).getByRole('button', { name: 'Import proxies' }),
  )
  await screen.findByText('Named exit')
  await user.click(screen.getByRole('button', { name: 'Test 127.0.0.1:18080' }))
  const report = await screen.findByRole('region', {
    name: 'IP report for 127.0.0.1:18080',
  })
  const lookup = await within(report).findByRole('button', {
    name: 'Check IP purity for 127.0.0.1:18080',
  })
  expect(
    fetch.mock.calls.some(([url]) => String(url).endsWith('/refresh')),
  ).toBe(false)
  await user.click(lookup)
  expect(await within(report).findByText('86')).toBeTruthy()
  expect(within(report).getByText('Clean')).toBeTruthy()
  expect(within(report).getByText('Residential')).toBeTruthy()
  expect(within(report).getByText('No risk flags')).toBeTruthy()
  // Each fact appears once per card even though the check and the report both describe it.
  expect(screen.getByText('AS64500 · Synthetic ISP')).toBeTruthy()
  expect(screen.getAllByText('203.0.113.8')).toHaveLength(1)
  expect(
    screen.getAllByText('Test city, Test region, United States'),
  ).toHaveLength(1)
  expect(
    screen.getAllByText('Test the connection to see the exit IP report.'),
  ).toHaveLength(1)
})

it('explains when the running backend lacks the bulk import route', async () => {
  const fetch = vi
    .fn()
    .mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/auth/state')
        return Promise.resolve(response(authenticated))
      if (url === '/api/proxies/import' && init?.method === 'POST')
        return Promise.resolve(response({ error: 'not_found' }, 404))
      if (url === '/api/proxies')
        return Promise.resolve(response({ proxies: [] }))
      return Promise.reject(new Error('Unexpected request'))
    })
  vi.stubGlobal('fetch', fetch)
  render(
    <App
      router={createAppRouter(
        createMemoryHistory({ initialEntries: ['/admin/proxies'] }),
      )}
    />,
  )
  const user = userEvent.setup()
  await screen.findByRole('heading', { name: 'Network proxies' })
  await user.click(screen.getByRole('button', { name: 'Import proxies' }))
  const dialog = await screen.findByRole('dialog')
  await user.type(
    within(dialog).getByLabelText('Proxy list'),
    'http://198.51.100.10:80',
  )
  await user.click(
    within(dialog).getByRole('button', { name: 'Import proxies' }),
  )
  await within(dialog).findByText(
    'Bulk import is unavailable on the running backend. Update or restart SubLane, then try again.',
  )
})
