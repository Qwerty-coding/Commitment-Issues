import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import Navbar from './navbar'

beforeEach(() => {
  globalThis.fetch = vi.fn(() =>
    Promise.resolve({
      ok: true,
      json: async () => ({
        success: true,
        data: { name: 'my-project', currentBranch: 'fix/conflicts' },
      }),
    })
  )
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('Navbar', () => {
  it('renders Commitment-Issues brand name and never old MergeSolver', async () => {
    render(<Navbar toggleSidebar={vi.fn()} sidebarOpen={true} />)
    expect(screen.getByText('Commitment-Issues')).toBeInTheDocument()
    expect(screen.queryByText(/MergeSolver/i)).toBeNull()
  })

  it('calls toggleSidebar when hamburger button is clicked', () => {
    const handleToggle = vi.fn()
    render(<Navbar toggleSidebar={handleToggle} sidebarOpen={false} />)
    const button = screen.getByRole('button', { name: /Toggle navigation menu/i })
    fireEvent.click(button)
    expect(handleToggle).toHaveBeenCalledTimes(1)
  })

  it('displays fetched repository and branch name', async () => {
    render(<Navbar toggleSidebar={vi.fn()} sidebarOpen={true} />)
    await waitFor(() => {
      expect(screen.getByText('my-project')).toBeInTheDocument()
      expect(screen.getByText('fix/conflicts')).toBeInTheDocument()
    })
  })
})

