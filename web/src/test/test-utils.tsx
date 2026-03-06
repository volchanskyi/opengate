import { render, type RenderOptions } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import type { ReactElement } from 'react';

export function renderWithRouter(
  element: ReactElement,
  { initialEntries = ['/'], ...options }: RenderOptions & { initialEntries?: string[] } = {},
) {
  const router = createMemoryRouter(
    [{ path: '*', element }],
    { initialEntries },
  );
  return render(<RouterProvider router={router} />, options);
}
