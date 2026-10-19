import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Conflicts from './conflicts'

const mockAnalysisData = [
  {
    file: 'src/main.py',
    hasConflicts: true,
    smartDiff: {
      collisions: [
        {
          kind: 'Function',
          name: 'process_data',
          type: 'COLLISION',
          line: 15,
          base_content: 'def process_data(): return []',
          our_content: 'def process_data(): return [1, 2]',
          their_content: 'def process_data(): return [3, 4]',
        },
      ],
      changes: [],
    },
  },
]

beforeEach(() => {
  globalThis.fetch = vi.fn(() =>
    Promise.resolve({
      ok: true,
      json: async () => ({ success: true, data: mockAnalysisData }),
    })
  )
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('Conflicts Tab', () => {
  it('renders the 3-way conflict view with Base, Ours, and Theirs panes', async () => {
    render(
      <MemoryRouter>
        <Conflicts />
      </MemoryRouter>
    )

    await waitFor(() => expect(screen.getAllByText('src/main.py').length).toBeGreaterThan(0))

    expect(screen.getAllByText(/process_data/).length).toBeGreaterThan(0)
    expect(screen.getByText('Base Version')).toBeInTheDocument()
    expect(screen.getByText('Ours Version')).toBeInTheDocument()
    expect(screen.getByText('Theirs Version')).toBeInTheDocument()

    // Syntax highlighting splits code into token spans, so assert on the
    // combined text content of the rendered panes rather than one node.
    const bodyText = document.body.textContent
    expect(bodyText).toContain('def process_data(): return []')
    expect(bodyText).toContain('def process_data(): return [1, 2]')
    expect(bodyText).toContain('def process_data(): return [3, 4]')
  })

  it('renders empty state when no conflicted files exist', async () => {
    globalThis.fetch = vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: async () => ({ success: true, data: [] }),
      })
    )

    render(
      <MemoryRouter>
        <Conflicts />
      </MemoryRouter>
    )

    await waitFor(() => {
      expect(screen.getByText('No conflicted files detected')).toBeInTheDocument()
    })
  })
})
