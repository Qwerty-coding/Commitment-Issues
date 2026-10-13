import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Sidebar from './sidebar'

describe('Sidebar', () => {
  it('renders all navigation links and updated titles', () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <Sidebar open={true} />
      </MemoryRouter>
    )

    expect(screen.getByText('Dashboard')).toBeInTheDocument()
    expect(screen.getByText('Conflicts')).toBeInTheDocument()
    expect(screen.getByText('TreeDiff')).toBeInTheDocument()
    expect(screen.getByText('Suggestions')).toBeInTheDocument()
    expect(screen.getByText('AST & Dependency Graph')).toBeInTheDocument()
    expect(screen.queryByText('Commit Graph')).toBeNull() // Was renamed from Commit Graph to AST & Dependency Graph
    expect(screen.getByText('Compare')).toBeInTheDocument()
    expect(screen.getByText('AI Context')).toBeInTheDocument()
    expect(screen.getByText('History')).toBeInTheDocument()
  })

  it('sets hidden class when open is false', () => {
    const { container } = render(
      <MemoryRouter initialEntries={['/']}>
        <Sidebar open={false} />
      </MemoryRouter>
    )

    const aside = container.querySelector('#app-sidebar')
    expect(aside).toHaveClass('sidebar-hidden')
  })

  it('does not have hidden class when open is true', () => {
    const { container } = render(
      <MemoryRouter initialEntries={['/']}>
        <Sidebar open={true} />
      </MemoryRouter>
    )

    const aside = container.querySelector('#app-sidebar')
    expect(aside).not.toHaveClass('sidebar-hidden')
  })
})

