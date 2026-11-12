import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { AllocationSettlement } from './AllocationSettlement'

const entry = {
  request_id: 'synthetic-image-edit',
  user_id: 2,
  mode: 'amount' as const,
  model: 'gpt-image-2',
  state: 'pending',
  input: 0,
  output: 0,
  cached: 0,
}

function mount(onSaved = vi.fn().mockResolvedValue(undefined)) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <AllocationSettlement id={9} entry={entry} onSaved={onSaved} />
    </QueryClientProvider>,
  )
  return onSaved
}

it('settles image input separately and rejects overlapping token categories', async () => {
  const bodies: unknown[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url === '/api/allocations/9/settle') {
        bodies.push(JSON.parse(String(init?.body)))
        return Promise.resolve(new Response(null, { status: 204 }))
      }
      return Promise.reject(new Error('Unexpected request'))
    }),
  )
  const onSaved = mount()
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Correct usage' }))
  const fill = async (label: string, value: string) => {
    const field = screen.getByLabelText(label)
    await user.clear(field)
    await user.type(field, value)
  }
  await fill('Input tokens · M', '1')
  await fill('Output tokens · M', '1')
  await fill('Cached input · M', '0.1')
  // Image and cached input together cannot exceed the total input.
  await fill('Image input · M', '0.95')
  await user.click(screen.getByRole('button', { name: 'Confirm settlement' }))
  expect(await screen.findByRole('alert')).toBeTruthy()
  expect(bodies).toHaveLength(0)
  await fill('Image input · M', '0.4')
  await user.click(screen.getByRole('button', { name: 'Confirm settlement' }))
  await waitFor(() => expect(onSaved).toHaveBeenCalled())
  expect(bodies).toEqual([
    {
      request_id: 'synthetic-image-edit',
      input: 1_000_000,
      output: 1_000_000,
      cached: 100_000,
      image_input: 400_000,
    },
  ])
})
