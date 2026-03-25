import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ErrorBoundary } from './ErrorBoundary';

function ThrowingComponent(): React.ReactNode {
  throw new Error('test error');
}

describe('ErrorBoundary', () => {
  it('renders children when no error', () => {
    render(
      <ErrorBoundary>
        <div>content</div>
      </ErrorBoundary>,
    );
    expect(screen.getByText('content')).toBeDefined();
  });

  it('renders fallback on error', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    render(
      <ErrorBoundary>
        <ThrowingComponent />
      </ErrorBoundary>,
    );
    expect(screen.getByText('Something went wrong')).toBeDefined();
    expect(screen.getByText('Reload page')).toBeDefined();
    vi.restoreAllMocks();
  });
});
