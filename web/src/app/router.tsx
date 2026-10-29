import { createBrowserRouter } from 'react-router-dom'
import { AppShell } from '../components/layout/AppShell'
import { RequireAuth } from '../lib/auth/RequireAuth'
import { Login } from '../features/auth/Login'
import { About } from '../features/about/About'
import { InventoryTable } from '../features/inventory/InventoryTable'
import { LabelView } from '../features/labels/LabelView'
import { OptionsManager } from '../features/options/OptionsManager'
import { ScanView } from '../features/scan/ScanView'
import { SettingsView } from '../features/settings/SettingsView'
import { PersonnelView } from '../features/personnel/PersonnelView'
import { UsersView } from '../features/users/UsersView'
import { ChangePasswordPage } from './pages/ChangePasswordPage'
import { NotFoundPage } from './pages/NotFoundPage'
import { Placeholder } from './pages/Placeholder'

/**
 * Application routes.
 *
 * Inventory, scan lookup, labels, About, and login are public.
 * The shell guard requires a session for all other routes. `/audit` is a placeholder: the audit log is
 * write-only (no read API yet) and is documented as a post-v1.0.0 follow-up.
 */
export const router = createBrowserRouter([
  { path: '/login', element: <Login /> },
  {
    path: '/',
    element: (
      <RequireAuth>
        <AppShell />
      </RequireAuth>
    ),
    children: [
      { index: true, element: <InventoryTable /> },
      { path: 'scan', element: <ScanView /> },
      { path: 'labels', element: <LabelView /> },
      { path: 'options', element: <OptionsManager /> },
      { path: 'settings', element: <SettingsView /> },
      { path: 'personnel', element: <PersonnelView /> },
      { path: 'users', element: <UsersView /> },
      { path: 'audit', element: <Placeholder titleKey="audit.title" implementsTask="post-v1.0.0" /> },
      { path: 'about', element: <About /> },
      { path: 'change-password', element: <ChangePasswordPage /> },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
])
