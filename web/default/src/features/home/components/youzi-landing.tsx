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
import type { CSSProperties } from 'react'
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

interface FloatingNode {
  id: string
  label: string
  x: string
  y: string
  size: number
  color: string
  delay: string
  duration: string
}

const TOP_NAV_LINKS = [
  { label: 'Home', href: '/' },
  { label: 'Console', href: '/dashboard' },
  { label: 'Model Market', href: '/pricing' },
  { label: 'Rankings', href: '/rankings' },
] as const

const FLOATING_NODES: FloatingNode[] = [
  {
    id: 'openai-a',
    label: '◎',
    x: '41%',
    y: '13%',
    size: 28,
    color: '#10a37f',
    delay: '0s',
    duration: '7.6s',
  },
  {
    id: 'claude-a',
    label: '✦',
    x: '16%',
    y: '18%',
    size: 34,
    color: '#d97757',
    delay: '-1.1s',
    duration: '8.2s',
  },
  {
    id: 'gemini-a',
    label: '✧',
    x: '58%',
    y: '15%',
    size: 26,
    color: '#8ab4f8',
    delay: '-2.2s',
    duration: '9s',
  },
  {
    id: 'meta-a',
    label: '∞',
    x: '68%',
    y: '23%',
    size: 36,
    color: '#1a73e8',
    delay: '-3.4s',
    duration: '8.8s',
  },
  {
    id: 'deepseek-a',
    label: '⌘',
    x: '31%',
    y: '31%',
    size: 22,
    color: '#4d6bfe',
    delay: '-1.8s',
    duration: '7.9s',
  },
  {
    id: 'qwen-a',
    label: '◇',
    x: '61%',
    y: '32%',
    size: 40,
    color: '#6657fa',
    delay: '-4.6s',
    duration: '9.4s',
  },
  {
    id: 'mistral-a',
    label: '✺',
    x: '76%',
    y: '39%',
    size: 24,
    color: '#f26e24',
    delay: '-2.9s',
    duration: '8.1s',
  },
  {
    id: 'azure-a',
    label: '▣',
    x: '12%',
    y: '41%',
    size: 30,
    color: '#00a4ef',
    delay: '-5s',
    duration: '8.7s',
  },
  {
    id: 'grok-a',
    label: '𝕏',
    x: '48%',
    y: '43%',
    size: 18,
    color: '#cbd5e1',
    delay: '-0.7s',
    duration: '7.4s',
  },
  {
    id: 'sora-a',
    label: '▻',
    x: '89%',
    y: '44%',
    size: 34,
    color: '#4d6bfe',
    delay: '-3.1s',
    duration: '9.1s',
  },
  {
    id: 'kimi-a',
    label: '☾',
    x: '23%',
    y: '55%',
    size: 20,
    color: '#a78bfa',
    delay: '-2.6s',
    duration: '8.5s',
  },
  {
    id: 'glm-a',
    label: '⌬',
    x: '39%',
    y: '62%',
    size: 32,
    color: '#00cc99',
    delay: '-1.6s',
    duration: '7.8s',
  },
  {
    id: 'cohere-a',
    label: '◆',
    x: '71%',
    y: '64%',
    size: 20,
    color: '#ff9db5',
    delay: '-3.8s',
    duration: '8.9s',
  },
  {
    id: 'midjourney-a',
    label: '⛵',
    x: '86%',
    y: '74%',
    size: 24,
    color: '#94a3b8',
    delay: '-4.4s',
    duration: '9.3s',
  },
  {
    id: 'baidu-a',
    label: '✣',
    x: '27%',
    y: '83%',
    size: 28,
    color: '#2932e1',
    delay: '-0.9s',
    duration: '8.4s',
  },
  {
    id: 'stability-a',
    label: '✹',
    x: '54%',
    y: '86%',
    size: 22,
    color: '#7a52ff',
    delay: '-2s',
    duration: '7.7s',
  },
  {
    id: 'openai-b',
    label: '◎',
    x: '81%',
    y: '90%',
    size: 31,
    color: '#10a37f',
    delay: '-3.5s',
    duration: '8.3s',
  },
]

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

function FloatingModelNodes() {
  return (
    <div
      aria-hidden='true'
      className='pointer-events-none absolute inset-0 z-[2] hidden lg:block'
    >
      {FLOATING_NODES.map((node) => (
        <div
          key={node.id}
          className='landing-orb-float absolute grid place-items-center rounded-full border border-white/20 bg-white/75 font-semibold text-slate-800 shadow-[0_0_22px_var(--node-color)] backdrop-blur-sm dark:bg-slate-950/70 dark:text-white'
          style={
            {
              left: node.x,
              top: node.y,
              width: node.size,
              height: node.size,
              color: node.color,
              '--node-color': `${node.color}88`,
              animationDelay: node.delay,
              animationDuration: node.duration,
            } as CSSProperties
          }
        >
          <span className='text-[0.78em]'>{node.label}</span>
        </div>
      ))}
    </div>
  )
}

export function YouziLanding(props: YouziLandingProps) {
  return (
    <main className='bg-background text-foreground relative h-svh min-h-svh w-full overflow-hidden'>
      <YouziStarTunnelBackground />
      <FloatingModelNodes />
      <LandingTopBar isAuthenticated={props.isAuthenticated} />

      <section className='relative z-10 flex h-svh min-h-svh flex-col items-center justify-center px-5 text-center'>
        <h1
          className='landing-animate-scale-in bg-gradient-to-br from-[#7cb342] via-[#fdd835] to-[#fb8c00] bg-clip-text text-[clamp(4.5rem,13vw,10rem)] leading-none font-normal tracking-[-0.04em] select-none dark:from-[#b3e85a] dark:via-[#ffe14d] dark:to-[#ffa733]'
          style={{
            fontFamily: 'Lobster, "Segoe Script", "Marker Felt", cursive',
            filter:
              'drop-shadow(0 0 26px rgba(255, 200, 60, 0.42)) drop-shadow(0 2px 6px rgba(0,0,0,0.18))',
          }}
        >
          Youziapi
        </h1>
      </section>
    </main>
  )
}
