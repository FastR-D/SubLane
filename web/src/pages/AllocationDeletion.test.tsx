import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryHistory } from '@tanstack/react-router'
import { expect, it, vi } from 'vitest'
import { App } from '@/App'
import { createAppRouter } from '@/router'
import { i18n } from '@/lib/i18n'
import { authenticated } from '@/test/fixtures'

it.each(['en', 'zh'])(
  'keeps an unsettled allowance and explains recovery in %s',
  async (language) => {
    await i18n.changeLanguage(language)
    const scheme = {
      id: 1,
      name: 'Synthetic allowance',
      group_id: 1,
      group_name: 'Synthetic group',
      enabled: true,
      created_at: 1900000000,
      effective_at: 1900000000,
      config: {
        mode: 'tokens',
        period: 'day',
        members: [{ user_id: 1, limit: 100 }],
        rates: [],
      },
      next: null,
    }
    const deletion = vi.fn()
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((url: string, init?: RequestInit) => {
        if (url === '/api/auth/state')
          return Promise.resolve(Response.json(authenticated))
        if (url === '/api/allocations/1' && init?.method === 'DELETE') {
          deletion()
          return Promise.resolve(
            Response.json({ error: 'allocation_pending' }, { status: 409 }),
          )
        }
        if (url === '/api/allocations')
          return Promise.resolve(Response.json({ schemes: [scheme] }))
        return Promise.resolve(Response.json({ groups: [] }))
      }),
    )
    render(
      <App
        router={createAppRouter(
          createMemoryHistory({ initialEntries: ['/admin/allocations'] }),
        )}
      />,
    )
    const user = userEvent.setup()
    await user.click(
      await screen.findByRole('button', {
        name: i18n.t('allocationDeleteNamed', { name: scheme.name }),
      }),
    )
    const dialog = await screen.findByRole('dialog')
    await user.click(
      within(dialog).getByRole('button', { name: i18n.t('allocationDelete') }),
    )
    expect((await within(dialog).findByRole('alert')).textContent).toBe(
      i18n.t('allocationDeletePending'),
    )
    expect(deletion).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('dialog')).toBe(dialog)
    expect(screen.getByText(scheme.name)).toBeTruthy()
  },
)
