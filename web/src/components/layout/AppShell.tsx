import { RentalProvider } from '../../features/rentals/RentalContext'
import { RentalBuffer } from '../../features/rentals/RentalBuffer'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/utils/cn'
import { useSession } from '../../lib/auth/sessionContext'
import { LanguageSwitcher } from '../../lib/i18n/LanguageSwitcher'
import { Button } from '../ui'

const NAV_ITEMS = [
  { to: '/', key: 'inventory', icon: 'M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z', admin: false },
  { to: '/scan', key: 'scan', icon: 'M8 3H3v5 M16 3h5v5 M3 16v5h5 M21 16v5h-5 M7 8v8 M10 8v8 M14 8v8 M17 8v8', admin: false },
  { to: '/labels', key: 'labels', icon: 'M3 3h9l9 9-9 9-9-9z M7 7h.01', admin: false },
  { to: '/about', key: 'about', icon: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18 M12 10v7 M12 7h.01', admin: false },
  { to: '/options', key: 'options', icon: 'M4 6h16 M4 12h16 M4 18h16 M8 3v6 M16 9v6 M9 15v6', admin: true },
  { to: '/settings', key: 'settings', icon: 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8 M9 3h6l1 3 3 1 2 5-2 5-3 1-1 3H9l-1-3-3-1-2-5 2-5 3-1z', admin: true },
  { to: '/personnel', key: 'personnel', icon: 'M3 21v-3a6 6 0 0 1 12 0v3 M9 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8', admin: true },
  { to: '/users', key: 'users', icon: 'M9 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8 M2 21v-3a6 6 0 0 1 12 0v3 M17 4a4 4 0 0 1 0 8 M18 15a5 5 0 0 1 4 5', admin: true },
]

export function AppShell() {
 const {user}=useSession(); return <RentalProvider key={user?.id??"public"}><ShellContent /></RentalProvider>
}
function ShellContent() {
  const { t } = useTranslation()
  const { user, logout } = useSession()
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const current = NAV_ITEMS.find(item => item.to === pathname)
  const canEdit = user?.role === 'admin'
  const visibleItems = NAV_ITEMS.filter(item => (item.key !== 'labels' || canEdit) && (!item.admin || user?.role === 'admin'))
  const role = user?.role ?? 'editor'
  const title = current ? t(`${current.key}.title`) : t('app.name')
  return (
    <div className="sklad-shell min-h-screen bg-canvas">
      <a href="#main-content" className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50 focus:rounded-md focus:bg-panel focus:px-4 focus:py-2 focus:text-sm focus:font-medium focus:text-content focus:shadow-overlay">{t('common.skipToContent')}</a>
      <aside className="sklad-sidebar">
        <NavLink to="/" className="pekat-wordmark"><img src="/brand/pekat-vision.svg" alt="PEKAT VISION" width="226" height="38" /></NavLink>
        <nav aria-label={t('app.name')}>
          <ul>
            {visibleItems.map((item, index) => (
              <li key={item.to}>
                {index === 0 || (item.admin && !visibleItems[index - 1]?.admin) ? <div className="nav-section">{t(item.admin ? 'shell.administration' : 'shell.workspace')}</div> : null}
                <NavLink to={item.to} end={item.to === '/'} className={({ isActive }) => cn('sidebar-link transition', isActive && 'is-active')}>
                  <svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={item.icon} /></svg>
                  <span>{t(`${item.key}.title`)}</span>
                </NavLink>
              </li>
            ))}
          </ul>
        </nav>
        <div className="sidebar-caption"><span aria-hidden="true">◈</span><p>{t(`shell.${role}Purpose`)}</p></div>
        <div className="sidebar-account"><span className="account-avatar" aria-hidden="true">{(user?.display_name || user?.username || 'S').slice(0, 1)}</span><div><strong title={user?.display_name || user?.username || t('shell.publicAccess')}>{user?.display_name || user?.username || t('shell.publicAccess')}</strong><small>{user ? t(`roles.${user.role}`) : t('roles.editor')}</small></div></div>
      </aside>
      <div className="sklad-workspace min-w-0">
        <header className="sklad-topbar border-b border-stroke bg-panel">
          <div className="min-w-0 truncate text-xs text-content-muted">{t('shell.workspace')} <span className="mx-3 opacity-40">/</span> <span className="text-content">{title}</span></div>
          <div className="flex min-w-0 flex-wrap items-center justify-end gap-2"><span className="role-chip">{t(`roles.${role}`)}</span><LanguageSwitcher />{user ? <Button variant="ghost" size="sm" onClick={() => void logout().then(() => navigate('/'))}>{t('auth.logout')}</Button> : <Button size="sm" onClick={() => navigate('/login', { state: { from: pathname } })}>{t('auth.signIn')}</Button>}</div>
        </header>
        <main id="main-content" tabIndex={-1} className="sklad-main min-w-0 focus:outline-none">
          {(pathname === '/' || pathname === '/scan') && <RentalBuffer />}
          <Outlet key={role} />
        </main>
        <footer className="sklad-footer flex flex-wrap items-center justify-between gap-3 border-t border-stroke text-xs text-content-muted">{user?.role === 'admin' || user?.must_change_password ? <NavLink to="/change-password" className="underline-offset-4 hover:underline">{t('auth.changePassword')}</NavLink> : null}<p>{t('app.name')} — {t('footer.madeBy')} · {t('footer.openSource')} · {t('common.license')}: MIT</p></footer>
      </div>
    </div>
  )
}
