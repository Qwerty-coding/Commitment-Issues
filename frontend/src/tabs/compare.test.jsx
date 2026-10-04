import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import Compare from './compare'

const baseResult = {
  repo1: '.',
  baseRef: 'abc123',
  oursRef: 'feature-a',
  theirsRef: 'feature-b',
  mergeBaseResolved: true,
  unrelatedRepositories: false,
  summary: {
    totalFiles: 1,
    oursOnly: 0,
    theirsOnly: 0,
    identical: 0,
    structuralCollisions: 1,
    contentConflicts: 0,
    addAddConflicts: 0,
    deleteModifyConflicts: 0,
    renameConflicts: 0,
    binaryConflicts: 0,
    unsupported: 0,
    errors: 0,
  },
  files: [
    {
      path: 'src/calc.js',
      status: 'structural_collision',
      explanation: '1 structural collision(s) detected: calculate (Function)',
      recommendation: 'Reconcile conflicting definitions',
      smartDiff: {
        collisions: [
          {
            type: 'COLLISION',
            kind: 'Function',
            name: 'calculate',
            line: 3,
            base_content: 'return x + 1',
            our_content: 'return x + 2',
            their_content: 'return x * 2',
          },
        ],
      },
    },
  ],
}

function mockFetch(payload, ok = true) {
  return vi.fn().mockResolvedValue({
    ok,
    status: ok ? 200 : 400,
    json: async () => payload,
  })
}

beforeEach(() => {
  globalThis.fetch = mockFetch({ success: true, data: baseResult })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('Compare tab', () => {
  it('renders the ref inputs', () => {
    render(<Compare />)
    expect(screen.getByLabelText(/Ours/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/Theirs/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Compare/i })).toBeInTheDocument()
  })

  it('rejects submission without both refs and never calls the API', () => {
    render(<Compare />)
    fireEvent.click(screen.getByRole('button', { name: /Compare/i }))
    expect(globalThis.fetch).not.toHaveBeenCalled()
    expect(screen.getByText(/required/i)).toBeInTheDocument()
  })

  it('posts the refs and renders the comparison result', async () => {
    render(<Compare />)
    fireEvent.change(screen.getByLabelText(/Ours/i), { target: { value: 'feature-a' } })
    fireEvent.change(screen.getByLabelText(/Theirs/i), { target: { value: 'feature-b' } })
    fireEvent.click(screen.getByRole('button', { name: /Compare/i }))

    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(1))

    const [url, options] = globalThis.fetch.mock.calls[0]
    expect(url).toContain('/api/compare')
    const body = JSON.parse(options.body)
    expect(body.oursRef).toBe('feature-a')
    expect(body.theirsRef).toBe('feature-b')

    await waitFor(() => expect(screen.getByText('src/calc.js')).toBeInTheDocument())
    expect(screen.getByText('Structural collision')).toBeInTheDocument()
    expect(screen.getAllByText(/calculate/).length).toBeGreaterThan(0)
    expect(screen.getByText('return x * 2')).toBeInTheDocument()
  })

  it('surfaces typed API errors', async () => {
    globalThis.fetch = mockFetch(
      { success: false, error: { code: 'COMPARE_REF_INVALID', message: 'bad ref' } },
      false,
    )
    render(<Compare />)
    fireEvent.change(screen.getByLabelText(/Ours/i), { target: { value: 'nope' } })
    fireEvent.change(screen.getByLabelText(/Theirs/i), { target: { value: 'nada' } })
    fireEvent.click(screen.getByRole('button', { name: /Compare/i }))

    await waitFor(() => expect(screen.getByText(/COMPARE_REF_INVALID/)).toBeInTheDocument())
  })
})
