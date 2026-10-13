import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Suggestions from './suggestions'

const mockSuggestions = [
  {
    id: 'sug-1',
    file: 'src/calc.js',
    provider: 'ollama',
    model: 'codellama',
    repository: 'my-repo',
    collisionKey: 'calculate-Function',
    collision: {
      name: 'calculate',
      kind: 'Function',
      line: 42,
    },
    resolutionId: 'res-1',
    status: 'proposed',
    resolution: {
      confidence_score: 88,
      explanation: 'Unified both addition and multiplication logic',
      resolved_code: 'function calculate(x) { return x * 2 + 1; }',
      conflict_type: 'Function',
    },
  },
]

const mockResolution = {
  resolutionId: 'res-1',
  status: 'proposed',
  approvalStatus: 'none',
  validationStatus: 'passed',
}

beforeEach(() => {
  globalThis.fetch = vi.fn((url) => {
    const urlStr = String(url)
    if (urlStr.includes('/api/suggestions')) {
      return Promise.resolve({
        ok: true,
        json: async () => ({ success: true, data: mockSuggestions, meta: { total: 1, failed: 0 } }),
      })
    }
    if (urlStr.includes('/api/resolutions/res-1/approve')) {
      return Promise.resolve({
        ok: true,
        json: async () => ({
          success: true,
          data: { ...mockResolution, approvalStatus: 'approved' },
        }),
      })
    }
    if (urlStr.includes('/api/resolutions/res-1')) {
      return Promise.resolve({
        ok: true,
        json: async () => ({ success: true, data: mockResolution }),
      })
    }
    return Promise.resolve({ ok: true, json: async () => ({ success: true, data: {} }) })
  })
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('Suggestions Tab', () => {
  it('renders suggestion details, deterministic verification badge, and uncalibrated model score', async () => {
    render(
      <MemoryRouter>
        <Suggestions />
      </MemoryRouter>
    )

    await waitFor(() => expect(screen.getByText('src/calc.js')).toBeInTheDocument())
    expect(screen.getByText(/calculate · Line 42/i)).toBeInTheDocument()

    // Check deterministic verification badge
    expect(screen.getByText('Syntax Checked')).toBeInTheDocument()

    // Check model score is explicitly labeled as Model Score (not blind ground-truth confidence)
    expect(screen.getByText(/Model Score: 88%/i)).toBeInTheDocument()
    expect(screen.getByText(/Unified both addition and multiplication/i)).toBeInTheDocument()
  })

  it('allows approving a suggested resolution', async () => {
    render(
      <MemoryRouter>
        <Suggestions />
      </MemoryRouter>
    )

    await waitFor(() => expect(screen.getByRole('button', { name: /Approve/i })).toBeInTheDocument())
    const approveBtn = screen.getByRole('button', { name: /Approve/i })
    fireEvent.click(approveBtn)

    await waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/resolutions/res-1/approve'),
        expect.objectContaining({ method: 'POST' })
      )
    })
  })

  it('displays provider unavailable error when code is PROVIDER_UNAVAILABLE', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 503,
      json: async () => ({
        success: false,
        error: { code: 'PROVIDER_UNAVAILABLE', message: 'Ollama is unreachable at localhost:11434' },
      }),
    })

    render(
      <MemoryRouter>
        <Suggestions />
      </MemoryRouter>
    )

    await waitFor(() => {
      expect(screen.getByText('AI Provider Unavailable')).toBeInTheDocument()
      expect(screen.getByText(/Ollama is unreachable/i)).toBeInTheDocument()
    })
  })

  it('renders deterministic verification score and findings checklist when provided', async () => {
    const verifiedSuggestion = [
      {
        ...mockSuggestions[0],
        id: 'sug-2',
        verification: {
          score: 65,
          status: 'needs_review',
          findings: [
            { check: 'markers', weight: 15, details: 'proposed content still contains conflict markers' },
            { check: 'preservation', weight: 20, details: 'lost essential imports' },
          ],
        },
      },
    ]

    globalThis.fetch = vi.fn((url) => {
      const urlStr = String(url)
      if (urlStr.includes('/api/suggestions')) {
        return Promise.resolve({
          ok: true,
          json: async () => ({ success: true, data: verifiedSuggestion, meta: { total: 1, failed: 0 } }),
        })
      }
      return Promise.resolve({ ok: true, json: async () => ({ success: true, data: {} }) })
    })

    render(
      <MemoryRouter>
        <Suggestions />
      </MemoryRouter>
    )

    await waitFor(() => expect(screen.getByText(/Verified: 65\/100/i)).toBeInTheDocument())
    expect(screen.getByText(/Deterministic Verification Findings/i)).toBeInTheDocument()
    expect(screen.getByText(/proposed content still contains conflict markers/i)).toBeInTheDocument()
    expect(screen.getByText(/lost essential imports/i)).toBeInTheDocument()
  })
})

