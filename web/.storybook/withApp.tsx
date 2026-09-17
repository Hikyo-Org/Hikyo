import type { Decorator } from '@storybook/react-vite'
import { QueryClientProvider } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { MemoryRouter, Outlet, Route, Routes } from 'react-router'

import { AuthProvider } from '../src/app/AuthProvider.tsx'
import { makeQueryClient } from '../src/app/queryClient.ts'
import { authenticatedIdentity } from '../src/testkit/identity.ts'

/**
 * The story harness for real route screens. It is the Storybook analogue of the
 * test suite's `inShell` helper (see e.g. `Projects.test.tsx`): a router with an
 * outlet context, a retry-free query client, and, behind `globalThis.fetch`, a
 * per-story table of canned API responses. Nothing here mocks a hook; screens
 * run their real data code against stubbed transport, exactly as in production.
 *
 * Stories opt in through `parameters.app`. A screen that reads `useOutletContext`
 * gets `outlet`; a screen that calls `useAuth` sets `auth: true` (which mounts
 * the real AuthProvider and answers its whoami from `identity`). Every API call
 * the screen makes must have a matching `responses` row, or the fetch stub
 * returns 404 and the screen's own error path shows, a missing route fails
 * loud rather than hanging.
 */

/** One canned response. `url` matches by exact path (string) or test (RegExp). */
export type MockRoute = {
  readonly url: string | RegExp
  /** HTTP verb to match. Defaults to GET; a non-GET request without a matching row 404s. */
  readonly method?: string
  readonly status?: number
  readonly body?: unknown
  /** Never settle, the screen's query stays pending, so its loading state is the story. */
  readonly pending?: boolean
}

export type AppParameters = {
  /** MemoryRouter initial entry. Default '/'. */
  readonly path?: string
  /** Route path the Story element is mounted at. Default matches `path`. */
  readonly routePath?: string
  /** Value handed to `useOutletContext`. */
  readonly outlet?: unknown
  /** Mount the real AuthProvider and answer whoami. */
  readonly auth?: boolean
  /** Identity the whoami stub returns when `auth`. Default authenticatedIdentity. */
  readonly identity?: unknown
  /** Canned API responses, matched in order. */
  readonly responses?: readonly MockRoute[]
}

function readRequest(input: RequestInfo | URL, init?: RequestInit): { url: string; method: string } {
  if (typeof input === 'string') {
    return { url: input, method: (init?.method ?? 'GET').toUpperCase() }
  }
  if (input instanceof URL) {
    return { url: input.href, method: (init?.method ?? 'GET').toUpperCase() }
  }
  // A Request carries its own method, but an init.method passed alongside it
  // still wins per the fetch spec, so honour the override rather than the Request's.
  return { url: input.url, method: (init?.method ?? input.method).toUpperCase() }
}

function matches(route: MockRoute, url: string, method: string): boolean {
  // A row defaults to GET, so a screen's mutation (DELETE/PATCH/…) 404s fail-loud
  // unless a story declares that verb explicitly, rather than silently taking a
  // same-URL GET fixture and masking an unintended write.
  if ((route.method ?? 'GET').toUpperCase() !== method) {
    return false
  }
  // A string route matches the request path exactly (so '/orgs/x/projects' does
  // not swallow '/orgs/x/projects/p/environments'); a RegExp matches the href.
  if (typeof route.url !== 'string') {
    // Reset lastIndex so a global/sticky RegExp does not alternate match/404 on
    // repeated identical requests within a story.
    route.url.lastIndex = 0
    return route.url.test(url)
  }
  return new URL(url, globalThis.location.origin).pathname === route.url
}

/** Build a fetch that answers from the table and 404s anything unmatched. */
function router(routes: readonly MockRoute[]): typeof fetch {
  return (input: RequestInfo | URL, init?: RequestInit) => {
    const { url, method } = readRequest(input, init)
    const route = routes.find((candidate) => matches(candidate, url, method))
    if (route === undefined) {
      return Promise.resolve(
        new Response(JSON.stringify({ error: `no story route for ${method} ${url}` }), {
          status: 404,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
    }
    // A pending row never resolves; the retry-free query client holds the
    // screen in its loading state, which is exactly what the story shows.
    if (route.pending === true) {
      return new Promise<Response>(() => {})
    }
    return Promise.resolve(
      new Response(route.body === undefined ? null : JSON.stringify(route.body), {
        status: route.status ?? 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
  }
}

/**
 * Preview `beforeEach`: install the per-story fetch table before the screen
 * mounts (its queries fire on mount, so a decorator effect would be too late),
 * and restore the real fetch afterwards. A whoami row is appended last for `auth`
 * screens so AuthProvider settles to the signed-in identity (a story's own
 * whoami row, matched first, still wins, Login supplies a 401).
 */
export async function installAppFetch(context: {
  parameters: { app?: AppParameters }
}): Promise<() => void> {
  const app = context.parameters.app
  if (app === undefined) {
    return () => {}
  }
  const routes: MockRoute[] = [...(app.responses ?? [])]
  if (app.auth === true) {
    // A last-resort whoami so AuthProvider settles signed-in; a story that wants
    // the anonymous state supplies its own whoami row (matched first) with 401.
    routes.push({ url: '/api/v1/auth/whoami', body: app.identity ?? authenticatedIdentity })
  }
  const original = globalThis.fetch
  globalThis.fetch = router(routes)
  return () => {
    globalThis.fetch = original
  }
}

function Shell({ app, children }: { app: AppParameters; children: ReactNode }) {
  const path = app.path ?? '/'
  const routePath = app.routePath ?? path
  return (
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route element={<Outlet context={app.outlet} />}>
          <Route path={routePath} element={children} />
        </Route>
      </Routes>
    </MemoryRouter>
  )
}

/**
 * The query provider for non-auth screens. `useState` keeps one client across
 * re-renders, the theme toolbar re-runs every decorator on each switch, and a
 * fresh client there would drop the cache and refetch mid-story. Uses the app's
 * own `makeQueryClient` defaults, so stories see production caching, not a copy.
 */
function Providers({ children }: { children: ReactNode }) {
  const [client] = useState(makeQueryClient)
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

/** Decorator: providers + router around a real route screen. */
export const withApp: Decorator = (Story, context) => {
  // Screens opt in via parameters.app; every other story renders untouched.
  if (context.parameters.app === undefined) {
    return <Story />
  }
  const app: AppParameters = context.parameters.app
  const shell = (
    <Shell app={app}>
      <Story />
    </Shell>
  )
  // AuthProvider owns its own retry-free query client, so it is the provider for
  // auth screens; no second QueryClientProvider around it.
  return app.auth === true ? <AuthProvider>{shell}</AuthProvider> : <Providers>{shell}</Providers>
}
