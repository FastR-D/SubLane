import { cleanup } from '@testing-library/react'
import { afterEach, vi } from 'vitest'
import { i18n } from '@/lib/i18n'

Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: vi.fn().mockImplementation((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })),
})
Object.defineProperty(window, 'scrollTo', { value: vi.fn(), writable: true })
afterEach(async () => {
  cleanup()
  // Radix FocusScope restores focus from a zero-delay timer after a dialog unmounts. Let it run
  // while this document still exists; otherwise it can fire into a torn-down jsdom window and
  // surface as an intermittent unhandled "dispatchEvent ... is not of type 'Event'" error.
  vi.useRealTimers()
  await new Promise((resolve) => setTimeout(resolve, 0))
  localStorage.clear()
  await i18n.changeLanguage('en')
})
