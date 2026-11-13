import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { accountSchema } from '@/lib/accounts'
import { AccountCredentialStatus } from './AccountCredentialStatus'

const now = 2_000_000_000
const account = accountSchema.parse({
  id: 'synthetic-account',
  name: 'Synthetic subscription',
  email: '',
  plan: '',
  enabled: false,
  status: 'ready',
  expires_at: now + 3600,
  created_at: now,
  updated_at: now,
  credential_refresh: {
    server_time: now,
    refreshing: false,
    failed: true,
    last_failure_at: now - 1,
    retry_at: now + 30,
  },
})

it('keeps a disabled valid token distinct from an early refresh failure', () => {
  render(<AccountCredentialStatus account={account} now={now} />)
  expect(screen.getByText('Disabled')).not.toBeNull()
  expect(screen.getByText('Verified')).not.toBeNull()
  expect(screen.getByText('Credential refresh failed')).not.toBeNull()
  expect(screen.getByText(/Earliest retry:/)).not.toBeNull()
  expect(screen.queryByText('Token expired')).toBeNull()
})

it('shows expiry only after the actual expiry and clears recovered failure feedback', () => {
  const { rerender } = render(
    <AccountCredentialStatus
      account={{ ...account, expires_at: now }}
      now={now}
    />,
  )
  expect(screen.getByText('Token expired')).not.toBeNull()
  expect(screen.getByText('Disabled')).not.toBeNull()
  expect(screen.queryByText('Verified')).toBeNull()
  rerender(
    <AccountCredentialStatus
      account={{
        ...account,
        credential_refresh: {
          ...account.credential_refresh!,
          failed: false,
          retry_at: 0,
          last_failure_at: 0,
        },
      }}
      now={now}
    />,
  )
  expect(screen.queryByText('Credential refresh failed')).toBeNull()
  expect(screen.queryByText(/Earliest retry:/)).toBeNull()
  expect(screen.getByText('Verified')).not.toBeNull()
})

it('does not hide reauthorization behind disablement or advertise automatic retries', () => {
  render(
    <AccountCredentialStatus
      account={{ ...account, status: 'reauth_required' }}
      now={now}
    />,
  )
  expect(screen.getByText('Disabled')).not.toBeNull()
  expect(screen.getByText('Reauthorization needed')).not.toBeNull()
  expect(screen.queryByText('Credential refresh failed')).toBeNull()
  expect(screen.queryByText(/Earliest retry:/)).toBeNull()
})

it('distinguishes an active rotation from waiting for another attempt', () => {
  const { rerender } = render(
    <AccountCredentialStatus
      account={{
        ...account,
        credential_refresh: {
          ...account.credential_refresh!,
          refreshing: true,
        },
      }}
      now={now}
    />,
  )
  expect(screen.getByText('Refreshing credentials')).not.toBeNull()
  expect(screen.queryByText(/Earliest retry:/)).toBeNull()
  rerender(<AccountCredentialStatus account={account} now={now + 31} />)
  expect(screen.queryByText('Refreshing credentials')).toBeNull()
  expect(
    screen.getByText('Waiting for the next refresh attempt.'),
  ).not.toBeNull()
})
