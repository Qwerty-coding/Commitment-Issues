import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import Dashboard from './dashboard'

beforeEach(() => {
  globalThis.fetch = vi.fn((url) => {
    if (String(url).includes('/api/repository')) {
      return Promise.resolve({
        ok: true,
        json: async () => ({ success: true, data: { name: 'demo', currentBranch: 'main' } }),
      })
    }
    return Promise.resolve({ ok: true, json: async () => ({ success: true, data: [] }) })
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('Dashboard', () => {
  it('greets the user without a hardcoded name', async () => {
    render(<Dashboard />)
    await waitFor(() => expect(screen.getByText('Welcome back')).toBeInTheDocument())
    expect(screen.queryByText(/Mahak/)).toBeNull()
  })

  it('renders no unicode emoji anywhere', async () => {
    render(<Dashboard />)
    await waitFor(() => expect(screen.getByText('Welcome back')).toBeInTheDocument())
    expect(document.body.textContent).not.toMatch(/[\u{1F300}-\u{1FAFF}\u{2600}-\u{27BF}]/u)
  })
})
