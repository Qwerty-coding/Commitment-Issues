import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import Commitgraph from './commitgraph'

const mockFullGraph = {
  nodes: [
    {
      data: {
        id: 'file:main.go',
        label: 'main.go',
        kind: 'file',
        status: 'file',
      },
    },
    {
      data: {
        id: 'fn:RunServer',
        label: 'RunServer',
        kind: 'Function',
        status: 'conflict',
        parent: 'file:main.go',
        base_code: 'func RunServer() {}',
        our_code: 'func RunServer() { startOurs() }',
        their_code: 'func RunServer() { startTheirs() }',
      },
    },
    {
      data: {
        id: 'fn:startOurs',
        label: 'startOurs',
        kind: 'Function',
        status: 'synced',
      },
    },
  ],
  edges: [
    {
      data: {
        id: 'e1',
        source: 'fn:RunServer',
        target: 'fn:startOurs',
        type: 'CALLS',
      },
    },
  ],
}

const mockFocusGraph = {
  nodes: [
    {
      data: {
        id: 'fn:RunServer',
        label: 'RunServer',
        kind: 'Function',
        status: 'conflict',
        our_code: 'func RunServer() { startOurs() }',
      },
    },
    {
      data: {
        id: 'fn:startOurs',
        label: 'startOurs',
        kind: 'Function',
        status: 'synced',
      },
    },
  ],
  edges: [
    {
      data: {
        id: 'e1',
        source: 'fn:RunServer',
        target: 'fn:startOurs',
        type: 'CALLS',
      },
    },
  ],
}

beforeEach(() => {
  globalThis.fetch = vi.fn((url) => {
    const urlStr = String(url)
    if (urlStr.includes('/api/graph/focus')) {
      return Promise.resolve({
        ok: true,
        json: async () => ({ success: true, message: 'focus', data: mockFocusGraph }),
      })
    }
    if (urlStr.includes('/api/graph')) {
      return Promise.resolve({
        ok: true,
        json: async () => ({ success: true, data: mockFullGraph }),
      })
    }
    return Promise.resolve({ ok: true, json: async () => ({ success: true, data: {} }) })
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('Commitgraph Tab', () => {
  it('renders AST nodes and semantic call edges', async () => {
    render(<Commitgraph />)

    await waitFor(() => expect(screen.getByText('AST & Dependency Graph')).toBeInTheDocument())
    expect(screen.getByText('main.go')).toBeInTheDocument()
    expect(screen.getByText('RunServer')).toBeInTheDocument()
    expect(screen.getByText(/Semantic Call Edges/i)).toBeInTheDocument()
  })

  it('allows focusing on a node and resetting view', async () => {
    render(<Commitgraph />)

    await waitFor(() => expect(screen.getByText('RunServer')).toBeInTheDocument())

    // Click focus on RunServer
    const focusButtons = screen.getAllByRole('button', { name: /Focus/i })
    fireEvent.click(focusButtons[0])

    await waitFor(() => {
      expect(
        globalThis.fetch.mock.calls.some((call) =>
          String(call[0]).includes('/api/graph/focus?id=')
        )
      ).toBe(true)
    })

    // Focus banner appears
    await waitFor(() => {
      expect(screen.getByText(/Focusing on symbol:/i)).toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: /Reset View/i })).toBeInTheDocument()

    // Click Reset View
    fireEvent.click(screen.getByRole('button', { name: /Reset View/i }))

    await waitFor(() => {
      expect(screen.queryByText(/Focusing on symbol:/i)).not.toBeInTheDocument()
    })
  })
})

