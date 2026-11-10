import {
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
  redirect,
  type RouterHistory,
} from '@tanstack/react-router'
import { AuthGate } from '@/components/AuthGate'
import { Auth } from '@/pages/Auth'
import { Invite } from '@/pages/Invite'
import { Setup } from '@/pages/Setup'
import { NotFound } from '@/pages/NotFound'

const root = createRootRoute({
  component: AuthGate,
  notFoundComponent: NotFound,
})
const settings = createRoute({
  getParentRoute: () => root,
  path: '/admin/settings',
  component: lazyRouteComponent(() => import('@/pages/System'), 'System'),
})
const routes = root.addChildren([
  createRoute({
    getParentRoute: () => root,
    path: '/channels/new',
    component: lazyRouteComponent(
      () => import('@/pages/ChannelCreate'),
      'ChannelCreate',
    ),
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/channels',
    component: lazyRouteComponent(() => import('@/pages/Channels'), 'Channels'),
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/admin/content',
    component: lazyRouteComponent(() => import('@/pages/Content'), 'Content'),
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/pricing',
    component: lazyRouteComponent(() => import('@/pages/Pricing'), 'Pricing'),
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/admin/allocations',
    component: lazyRouteComponent(
      () => import('@/pages/Allocations'),
      'Allocations',
    ),
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/admin/instance',
    component: lazyRouteComponent(() => import('@/pages/Overview'), 'Overview'),
  }),
  settings.addChildren([
    createRoute({
      getParentRoute: () => settings,
      path: 'updates',
      component: lazyRouteComponent(() => import('@/pages/Updates'), 'Updates'),
    }),
    createRoute({
      getParentRoute: () => settings,
      path: 'turnstile',
      component: lazyRouteComponent(
        () => import('@/pages/System'),
        'TurnstilePage',
      ),
    }),
    createRoute({
      getParentRoute: () => settings,
      path: 'alerts',
      component: lazyRouteComponent(
        () => import('@/pages/System'),
        'AlertSettings',
      ),
    }),
    createRoute({
      getParentRoute: () => settings,
      path: 'backup',
      component: lazyRouteComponent(() => import('@/pages/Backup'), 'Backup'),
    }),
    createRoute({
      getParentRoute: () => settings,
      path: '/',
      beforeLoad: () => {
        // Redirect before rendering so an old index page cannot redirect a later navigation.
        throw redirect({ to: '/admin/settings/clients', replace: true })
      },
    }),
    createRoute({
      getParentRoute: () => settings,
      path: 'timezone',
      component: lazyRouteComponent(
        () => import('@/pages/System'),
        'TimeZonePage',
      ),
    }),
    createRoute({
      getParentRoute: () => settings,
      path: 'clients',
      component: lazyRouteComponent(
        () => import('@/pages/System'),
        'ClientVersions',
      ),
    }),
    ...(['codex', 'grok', 'claude'] as const).map((path) =>
      createRoute({
        getParentRoute: () => settings,
        path,
        beforeLoad: () => {
          // Keep saved provider links working after consolidating version settings.
          throw redirect({ to: '/admin/settings/clients', replace: true })
        },
      }),
    ),
  ]),
  createRoute({
    getParentRoute: () => root,
    path: '/admin/audit',
    component: lazyRouteComponent(() => import('@/pages/Audit'), 'Audit'),
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/admin/proxies',
    component: lazyRouteComponent(() => import('@/pages/Proxies'), 'Proxies'),
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/usage',
    beforeLoad: () => {
      // Keep saved personal usage links working after moving the report home.
      throw redirect({ to: '/', replace: true })
    },
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/admin/usage',
    component: lazyRouteComponent(() => import('@/pages/Usage'), 'TeamUsage'),
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/setup',
    component: Setup,
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/setup/admin',
    component: () => <Auth mode="setup" />,
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/login',
    component: () => <Auth mode="login" />,
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/invite',
    component: Invite,
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/',
    component: lazyRouteComponent(() => import('@/pages/Home'), 'Home'),
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/accounts',
    component: lazyRouteComponent(() => import('@/pages/Accounts'), 'Accounts'),
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/keys',
    component: lazyRouteComponent(() => import('@/pages/Keys'), 'Keys'),
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/requests',
    component: lazyRouteComponent(() => import('@/pages/Requests'), 'Requests'),
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/admin/requests',
    component: lazyRouteComponent(
      () => import('@/pages/Requests'),
      'AllRequests',
    ),
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/groups',
    component: lazyRouteComponent(() => import('@/pages/Groups'), 'Groups'),
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/groups/scheduling',
    validateSearch: (search: Record<string, unknown>) => ({
      group:
        Number.isInteger(Number(search.group)) && Number(search.group) > 0
          ? Number(search.group)
          : undefined,
      model:
        typeof search.model === 'string'
          ? search.model.slice(0, 160)
          : undefined,
    }),
    component: lazyRouteComponent(
      () => import('@/pages/Scheduling'),
      'Scheduling',
    ),
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/members',
    component: lazyRouteComponent(() => import('@/pages/Members'), 'Members'),
  }),
  createRoute({
    getParentRoute: () => root,
    path: '/preferences',
    component: lazyRouteComponent(() => import('@/pages/Settings'), 'Settings'),
  }),
])

export function createAppRouter(history?: RouterHistory) {
  return createRouter({ routeTree: routes, history, defaultPreload: 'intent' })
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof createAppRouter>
  }
}
