import { useTranslation } from 'react-i18next'
import { useSession } from '../../lib/auth/sessionContext'
import { Card, PageHeader } from '../../components/ui'
import { Link } from 'react-router-dom'
export function UsersView(){const {t}=useTranslation();const {user}=useSession();return <div className="space-y-4"><PageHeader title={t('users.title')} description={t('rental.accountHelp')}/><Card className="space-y-3 p-5"><p className="font-semibold text-content">{user?.display_name || user?.username} · {t('roles.admin')}</p><p className="text-sm text-content-muted">{user?.username}</p><Link className="text-sm underline" to="/change-password">{t('auth.changePassword')}</Link></Card></div>}
