import './App.css'
import Navbar from './components/navbar'
import Sidebar from './components/sidebar'
import Dashboard from './tabs/dashboard'
import Conflicts from './tabs/conflicts'
import Treediff from './tabs/treediff'
import Suggestions from './tabs/suggestions'
import Commitgraph from './tabs/commitgraph'
import History from './tabs/history'
import AIContext from './tabs/aicontext'
import Compare from './tabs/compare'
import ErrorBoundary from './components/ErrorBoundary'
import NotFound from './components/NotFound'

import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { useState } from 'react'

function App() {
  const [sidebarOpen, setSidebarOpen] = useState(false)

  const toggleSidebar = () => {
    setSidebarOpen((prev) => !prev)
  }

  return (
    <BrowserRouter>
      <ErrorBoundary>
        <Navbar toggleSidebar={toggleSidebar} sidebarOpen={sidebarOpen} />

        <div className="app-layout">
          <Sidebar open={sidebarOpen} />

          <div className="page-area">
            <ErrorBoundary>
              <Routes>
                <Route path="/" element={<Dashboard />} />
                <Route path="/suggestions" element={<Suggestions />} />
                <Route path="/conflicts" element={<Conflicts />} />
                <Route path="/treediff" element={<Treediff />} />
                <Route path="/commitgraph" element={<Commitgraph />} />
                <Route path="/history" element={<History />} />
                <Route path="/context" element={<AIContext />} />
                <Route path="/compare" element={<Compare />} />
                <Route path="*" element={<NotFound />} />
              </Routes>
            </ErrorBoundary>
          </div>

        </div>
      </ErrorBoundary>
    </BrowserRouter>
  )
}

export default App
