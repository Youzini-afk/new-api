/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Link } from '@tanstack/react-router'
import { Languages, Moon, Sun } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { NotificationPopover } from '@/components/notification-popover'
import { ProfileDropdown } from '@/components/profile-dropdown'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useTheme } from '@/context/theme-provider'
import { useNotifications } from '@/hooks/use-notifications'
import { INTERFACE_LANGUAGE_OPTIONS } from '@/i18n/languages'
import { useAuthStore } from '@/stores/auth-store'

import { YouziStarTunnelBackground } from './youzi-star-tunnel-background'

interface YouziLandingProps {
  isAuthenticated: boolean
}

const TOP_NAV_LINKS = [
  { label: 'Home', href: '/' },
  { label: 'Console', href: '/dashboard' },
  { label: 'Model Market', href: '/pricing' },
  { label: 'Rankings', href: '/rankings' },
] as const

function ThemeControl() {
  const { resolvedTheme, setTheme } = useTheme()
  const { t } = useTranslation()
  const isDark = resolvedTheme === 'dark'
  const nextLabel = isDark
    ? t('Switch to light mode')
    : t('Switch to dark mode')

  return (
    <button
      type='button'
      onClick={() => setTheme(isDark ? 'light' : 'dark')}
      className='text-foreground/90 hover:bg-foreground/8 grid size-9 place-items-center rounded-full transition-colors dark:text-white dark:hover:bg-white/10'
      aria-label={nextLabel}
    >
      {isDark ? <Moon className='size-5' /> : <Sun className='size-5' />}
    </button>
  )
}

function LanguageControl() {
  const { i18n, t } = useTranslation()
  const currentLanguage = i18n.language?.startsWith('zh') ? '中文' : 'EN'

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger
        render={
          <button
            type='button'
            className='text-foreground/90 hover:bg-foreground/8 grid size-9 place-items-center rounded-full transition-colors dark:text-white dark:hover:bg-white/10'
            aria-label={t('Language')}
          />
        }
      >
        <Languages className='size-5' />
        <span className='sr-only'>{currentLanguage}</span>
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end' className='min-w-36'>
        {INTERFACE_LANGUAGE_OPTIONS.map((lang) => (
          <DropdownMenuItem
            key={lang.code}
            onClick={() => i18n.changeLanguage(lang.code)}
          >
            {lang.label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function LandingTopBar(props: { isAuthenticated: boolean }) {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const notifications = useNotifications()

  return (
    <header className='absolute top-4 right-4 z-30 sm:top-5 sm:right-7'>
      <nav
        aria-label={t('Main navigation')}
        className='text-foreground/85 flex max-w-[calc(100vw-2rem)] items-center gap-1 rounded-full border border-transparent bg-transparent px-1 py-1 text-sm font-semibold backdrop-blur-[2px] dark:text-white/90'
      >
        <div className='hidden items-center gap-1 sm:flex'>
          {TOP_NAV_LINKS.map((link) => (
            <Link
              key={link.href}
              to={link.href}
              className='hover:bg-foreground/8 hover:text-foreground rounded-full px-3 py-2 transition-colors dark:hover:bg-white/10 dark:hover:text-white'
            >
              {t(link.label)}
            </Link>
          ))}
          <span
            aria-hidden='true'
            className='bg-foreground/10 mx-2 h-5 w-px dark:bg-white/10'
          />
        </div>

        <LanguageControl />
        <ThemeControl />

        {props.isAuthenticated && user ? (
          <>
            <NotificationPopover
              open={notifications.popoverOpen}
              onOpenChange={notifications.setPopoverOpen}
              unreadCount={notifications.unreadCount}
              activeTab={notifications.activeTab}
              onTabChange={notifications.setActiveTab}
              notice={notifications.notice}
              announcements={notifications.announcements}
              loading={notifications.loading}
            />
            <ProfileDropdown />
          </>
        ) : (
          <Link
            to='/sign-in'
            className='hover:bg-foreground/8 hover:text-foreground rounded-full px-3 py-2 text-xs font-semibold transition-colors sm:text-sm dark:hover:bg-white/10 dark:hover:text-white'
          >
            {t('Sign In')}
          </Link>
        )}
      </nav>
    </header>
  )
}

export function YouziLanding(props: YouziLandingProps) {
  return (
    <main className='bg-background text-foreground relative h-svh min-h-svh w-full overflow-hidden'>
      <YouziStarTunnelBackground />
      <LandingTopBar isAuthenticated={props.isAuthenticated} />

      <section className='pointer-events-none relative z-10 flex h-svh min-h-svh flex-col items-center justify-center px-5 text-center'>
        <h1
          className='landing-animate-scale-in inline-block overflow-visible bg-gradient-to-r from-[#4db6ac] via-[#c5e1a5] to-[#fdd835] bg-clip-text pt-[0.3em] pb-[0.14em] text-[clamp(4.5rem,13vw,10rem)] leading-[1.32] font-normal tracking-[-0.04em] text-transparent select-none dark:from-[#80cbc4] dark:via-[#dcedc8] dark:to-[#ffe14d]'
          style={{
            fontFamily: 'Lobster, "Segoe Script", "Marker Felt", cursive',
            filter:
              'drop-shadow(0 0 26px rgba(255, 200, 60, 0.42)) drop-shadow(0 2px 6px rgba(0,0,0,0.18))',
          }}
        >
          Youzi
        </h1>
      </section>
    </main>
  )
}
