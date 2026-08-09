import './App.css'
import Navbar from './components/navbar'
import Sidebar from "./components/sidebar"
//tabs on sidebar
import Dashboard from './tabs/dashboard' 
import Conflicts from './tabs/conflicts'
import Treediff from './tabs/treediff'
import Suggestions from './tabs/suggestions'
import Commitgraph from './tabs/commitgraph'
import History from './tabs/history'

import {BrowserRouter, Routes, Route} from "react-router-dom"
import { useState} from "react";



function App() {
  const [sidebarOpen, setSidebarOpen] = useState(false);

  // function to toggle sidebar
    const toggleSidebar = () => {
        setSidebarOpen(prev => !prev);
    };

  return (
    <BrowserRouter>
    
    <Navbar toggleSidebar={toggleSidebar}/>

    <div className="app-layout">

    {/* SIDEBAR */}
    {sidebarOpen && (
        <Sidebar />
    )}

    {/* PAGE AREA */}
    <div className="page-area">

        <Routes>

            <Route
                path="/"
                element={<Dashboard />}
            />

            <Route
                path="/suggestions"
                element={<Suggestions />}
            />

            <Route
                path="/conflicts"
                element={<Conflicts />}
            />

        </Routes>

    </div>

</div>
</BrowserRouter>
  )
}

export default App
