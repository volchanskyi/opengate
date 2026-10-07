import { render } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { routeCrumbOf, useRouteCrumb } from './use-route-crumb';

function Page({ label }: { readonly label: string | undefined }) {
  useRouteCrumb(label);
  return null;
}

function routerFor(label: string | undefined, state?: unknown) {
  const router = createMemoryRouter(
    [{ path: 'devices/:id', element: <Page label={label} /> }],
    { initialEntries: [{ pathname: '/devices/d1', state }] },
  );
  render(<RouterProvider router={router} />);
  return router;
}

describe('useRouteCrumb', () => {
  it('writes the page label into its own history entry, keeping the entry in place', () => {
    const router = routerFor('reception-pc');
    expect(routeCrumbOf(router.state.location.state)).toBe('reception-pc');
    expect(router.state.location.pathname).toBe('/devices/d1');
    expect(router.state.historyAction).toBe('REPLACE');
  });

  it('keeps whatever else the entry carried', () => {
    const router = routerFor('reception-pc', { relayUrl: 'wss://relay.example.com' });
    expect(router.state.location.state).toEqual({ relayUrl: 'wss://relay.example.com', crumb: 'reception-pc' });
  });

  it('leaves the entry alone until the page has a label', () => {
    const router = routerFor(undefined);
    expect(routeCrumbOf(router.state.location.state)).toBeUndefined();
    expect(router.state.historyAction).toBe('POP');
  });
});

describe('routeCrumbOf', () => {
  it('reads a label only when the state carries one as text', () => {
    expect(routeCrumbOf({ crumb: 'web-01' })).toBe('web-01');
    expect(routeCrumbOf({ crumb: 42 })).toBeUndefined();
    expect(routeCrumbOf(null)).toBeUndefined();
    expect(routeCrumbOf('web-01')).toBeUndefined();
  });
});
