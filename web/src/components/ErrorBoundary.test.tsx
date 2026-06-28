import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ErrorBoundary } from './ErrorBoundary';
import * as reporter from '../lib/report-error';

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

  it('reports the caught error once, PII-free', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    const spy = vi.spyOn(reporter, 'reportClientError').mockReturnValue(true);
    render(
      <ErrorBoundary>
        <ThrowingComponent />
      </ErrorBoundary>,
    );
    expect(spy).toHaveBeenCalledTimes(1);
    const arg = spy.mock.calls[0]![0];
    expect(arg.message).toBe('test error');
    expect(arg.source).toBe('ErrorBoundary');
    expect(arg).not.toHaveProperty('email');
    expect(arg).not.toHaveProperty('token');
    vi.restoreAllMocks();
  });
});
