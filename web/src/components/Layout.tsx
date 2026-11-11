import { DemoNotice } from './DemoNotice'
import { useId, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, Outlet, useRouterState } from '@tanstack/react-router'
import {
  Activity,
  FolderClosed,
  Gauge,
  ChevronRight,
  ShieldCheck,
  ChartNoAxesCombined,
  CircleDollarSign,
  ListChecks,
  KeyRound,
  LayoutDashboard,
  Route,
  Settings,
  Users,
  Workflow,
  Plug,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { authOptions } from '@/lib/auth'
import { canAccess } from '@/lib/access'
import { selectedWorkspace } from '@/lib/workspace'
import { cn } from '@/lib/cn'
import { LanguageSelect, ThemeSelect } from './Preferences'
import { Logo } from './Logo'
import { Session } from './Session'
import { WorkspaceSelect } from './WorkspaceSelect'
import { Button } from './ui/Button'
import { TourProvider, TourButton } from './TourProvider'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarProvider,
  SidebarTrigger,
  useSidebar,
} from './ui/Sidebar'

const navigation = [
  {
    label: 'general',
    items: [
      { to: '/', label: 'overview', icon: LayoutDashboard },
      { to: '/keys', label: 'apiKeys', icon: KeyRound },
      { to: '/requests', label: 'requests', icon: ListChecks },
      { to: '/pricing', label: 'modelPrices', icon: CircleDollarSign },
    ],
  },
  {
    label: 'administration',
    items: [
      { to: '/accounts', label: 'accounts', icon: Workflow },
      { to: '/channels', label: 'channelsTitle', icon: Plug },
      { to: '/admin/proxies', label: 'proxiesTitle', icon: Route },
      { to: '/groups', label: 'accountGroups', icon: FolderClosed },
      { to: '/admin/requests', label: 'allRequests', icon: ListChecks },
      { to: '/admin/usage', label: 'teamUsage', icon: ChartNoAxesCombined },
      { to: '/admin/instance', label: 'instanceStatus', icon: Activity },
      { to: '/members', label: 'members', icon: Users },
      { to: '/admin/allocations', label: 'allocationSchemes', icon: Gauge },
      { to: '/admin/audit', label: 'auditLog', icon: ShieldCheck },
      { to: '/admin/content', label: 'contentTitle', icon: ShieldCheck },
      {
        to: '/admin/settings',
        label: 'systemSettings',
        icon: Settings,
        children: [
          { to: '/admin/settings/alerts', label: 'alertsTitle' },
          { to: '/admin/settings/timezone', label: 'timeZoneTitle' },
          { to: '/admin/settings/turnstile', label: 'turnstileTitle' },
          { to: '/admin/settings/clients', label: 'clientVersionsTitle' },
          { to: '/admin/settings/updates', label: 'updatesTitle' },
          { to: '/admin/settings/backup', label: 'backupTitle' },
        ],
      },
    ],
  },
] as const

function NavigationItem({
  item,
  pathname,
  allowed,
}: {
  item: (typeof navigation)[number]['items'][number]
  pathname: string
  allowed: (path: string) => boolean
}) {
  const { t } = useTranslation()
  const { setOpenMobile } = useSidebar()
  const id = useId()
  const active = pathname === item.to || pathname.startsWith(item.to + '/')
  const [disclosure, setDisclosure] = useState({ pathname, expanded: active })
  // A new route reveals its active branch without overriding a manual collapse on this page.
  const expanded =
    disclosure.pathname === pathname ? disclosure.expanded : active
  const Icon = item.icon
  return (
    <SidebarMenuItem>
      {'children' in item ? (
        <>
          <SidebarMenuButton
            className="h-10"
            aria-expanded={expanded}
            aria-controls={id}
            onClick={() => setDisclosure({ pathname, expanded: !expanded })}
          >
            <Icon aria-hidden="true" />
            <span>{t(item.label)}</span>
            <ChevronRight
              aria-hidden="true"
              className={`ml-auto shrink-0 motion-safe:transition-transform ${expanded ? 'rotate-90' : ''}`}
            />
          </SidebarMenuButton>
          <SidebarMenuSub
            id={id}
            hidden={!expanded}
            className={!expanded ? 'hidden' : undefined}
          >
            {item.children
              .filter((child) => allowed(child.to))
              .map((child) => (
                <SidebarMenuSubItem key={child.to}>
                  <SidebarMenuSubButton
                    asChild
                    isActive={pathname === child.to}
                    className="h-auto min-h-9 py-2 [@media(pointer:coarse)]:min-h-11"
                  >
                    <Link
                      to={child.to}
                      aria-current={pathname === child.to ? 'page' : undefined}
                      onClick={() => setOpenMobile(false)}
                    >
                      <span className="whitespace-normal break-words">
                        {t(child.label)}
                      </span>
                    </Link>
                  </SidebarMenuSubButton>
                </SidebarMenuSubItem>
              ))}
          </SidebarMenuSub>
        </>
      ) : (
        <SidebarMenuButton asChild isActive={active} className="h-10">
          <Link
            to={item.to}
            onClick={() => setOpenMobile(false)}
            aria-current={active ? 'page' : undefined}
          >
            <Icon aria-hidden="true" />
            <span>{t(item.label)}</span>
          </Link>
        </SidebarMenuButton>
      )}
    </SidebarMenuItem>
  )
}

function Navigation() {
  const { t } = useTranslation()
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const { setOpenMobile } = useSidebar()
  const { data } = useQuery(authOptions())
  const allowed = (path: string) =>
    canAccess(
      path,
      data?.user?.role,
      data?.user?.id === 1 && selectedWorkspace() === 1,
    )
  return (
    <Sidebar className="border-e border-border">
      <SidebarHeader className="px-3 py-4">
        <div className="flex min-w-0 items-center gap-2">
          <Link
            to="/"
            className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-foreground text-background focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring [@media(pointer:coarse)]:size-11"
            aria-label="SubLane"
            onClick={() => setOpenMobile(false)}
          >
            <Logo className="size-6" />
          </Link>
          <WorkspaceSelect />
        </div>
      </SidebarHeader>
      <SidebarContent className="gap-5">
        {navigation.map((group) => {
          const items = group.items.filter((item) =>
            'children' in item
              ? item.children.some((child) => allowed(child.to))
              : allowed(item.to),
          )
          if (!items.length) return null
          return (
            <SidebarGroup
              key={group.label}
              role="group"
              aria-label={t(group.label)}
              className="px-3"
            >
              <SidebarGroupLabel>{t(group.label)}</SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu>
                  {items.map((item) => (
                    <NavigationItem
                      key={item.to}
                      item={item}
                      pathname={pathname}
                      allowed={allowed}
                    />
                  ))}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          )
        })}
      </SidebarContent>
      <SidebarFooter className="p-3">
        <Session />
      </SidebarFooter>
    </Sidebar>
  )
}

export function Layout({ children }: { children?: ReactNode }) {
  const { t } = useTranslation()
  const { data } = useQuery(authOptions())
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  })
  const tablePage =
    !children &&
    [
      '/requests',
      '/admin/requests',
      '/groups/scheduling',
      '/admin/audit',
      '/members',
      '/keys',
      '/pricing',
    ].includes(pathname.replace(/\/+$/, ''))
  const user = data?.user
  const workspace = selectedWorkspace()
  if (!user) return null
  // Scope changes must also clear route state, query placeholders and open dialogs.
  return (
    <TourProvider
      key={`${user.id}:${user.role}:${workspace}`}
      userID={user.id}
      administrator={user.role === 'admin'}
      workspace={workspace}
    >
      <SidebarProvider>
        <a
          href="#main-content"
          className="sr-only z-50 rounded-md bg-primary p-3 text-primary-foreground focus:not-sr-only focus:fixed focus:left-4 focus:top-4"
        >
          {t('skipContent')}
        </a>
        <Navigation />
        <SidebarInset
          className={cn('min-w-0 bg-background', tablePage && 'h-dvh')}
        >
          <header className="flex min-h-16 shrink-0 items-center justify-between gap-3 px-4 md:px-6">
            <div className="flex items-center gap-3">
              <SidebarTrigger className="size-7 text-muted-foreground [@media(pointer:coarse)]:size-11" />
            </div>
            <div className="flex items-center gap-1">
              <LanguageSelect compact />
              <ThemeSelect compact />
              <TourButton />
              <Button
                asChild
                variant="ghost"
                size="icon"
                className="text-muted-foreground [@media(pointer:coarse)]:size-11"
              >
                <Link
                  to="/preferences"
                  aria-label={t('preferences')}
                  title={t('preferences')}
                >
                  <Settings aria-hidden="true" />
                </Link>
              </Button>
            </div>
          </header>
          <DemoNotice />
          <main
            id="main-content"
            className={cn(
              'mx-auto w-full px-5 py-6 md:px-6',
              // Tables need a bounded parent to keep scrollbars and pagination in the viewport.
              tablePage
                ? 'flex min-h-0 flex-1 flex-col overflow-y-auto'
                : 'max-w-6xl',
            )}
          >
            {children ?? <Outlet />}
          </main>
        </SidebarInset>
      </SidebarProvider>
    </TourProvider>
  )
}
