import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi } from 'vitest';
import { SessionToolbar } from './SessionToolbar';

describe('SessionToolbar', () => {
  it.each([
    ['connecting', 'Connecting...', 'bg-yellow-500'],
    ['connected', 'Connected', 'bg-green-500'],
    ['disconnected', 'Disconnected', 'bg-gray-500'],
    ['error', 'Error', 'bg-red-500'],
  ] as const)('shows "%s" label and %s indicator for %s state', (state, expected, expectedColor) => {
    const { container } = render(<SessionToolbar connectionState={state} title="FS01 — Contoso" path={{ direct: false }} onDisconnect={vi.fn()} />);
    expect(screen.getByText(expected)).toBeInTheDocument();
    const dot = container.querySelector('.rounded-full');
    expect(dot?.className).toContain(expectedColor);
  });

  it('shows "Unknown" label for an unrecognized state value', () => {
    const { container } = render(
      <SessionToolbar connectionState={'totally-bogus' as unknown as 'connected'} title="FS01 — Contoso" path={{ direct: false }} onDisconnect={vi.fn()} />,
    );
    expect(screen.getByText('Unknown')).toBeInTheDocument();
    expect(container.querySelector('.rounded-full')?.className).toContain('bg-gray-500');
  });

  it('calls onDisconnect when disconnect button is clicked', async () => {
    const user = userEvent.setup();
    const onDisconnect = vi.fn();
    render(<SessionToolbar connectionState="connected" title="FS01 — Contoso" path={{ direct: false }} onDisconnect={onDisconnect} />);

    await user.click(screen.getByText('Disconnect'));
    expect(onDisconnect).toHaveBeenCalledOnce();
  });

  it('renders disconnect button', () => {
    render(<SessionToolbar connectionState="connected" title="FS01 — Contoso" path={{ direct: false }} onDisconnect={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Disconnect' })).toBeInTheDocument();
  });

  it('names the host and its customer', () => {
    render(<SessionToolbar connectionState="connected" title="FS01 — Contoso" path={{ direct: true }} onDisconnect={vi.fn()} />);
    expect(screen.getByText('FS01 — Contoso')).toBeInTheDocument();
  });

  it('shows Direct when the session runs over the direct link', () => {
    render(<SessionToolbar connectionState="connected" title="FS01 — Contoso" path={{ direct: true }} onDisconnect={vi.fn()} />);
    expect(screen.getByText('Direct')).toBeInTheDocument();
    expect(screen.queryByText('Through server')).not.toBeInTheDocument();
  });

  it('shows Through server with the reason on hover, in plain words', () => {
    render(
      <SessionToolbar
        connectionState="connected"
        title="FS01 — Contoso"
        path={{ direct: false, reason: 'no direct route on this network' }}
        onDisconnect={vi.fn()}
      />,
    );
    const label = screen.getByText('Through server');
    expect(label).toHaveAttribute('title', 'no direct route on this network');
    expect(label).toHaveAccessibleName('Through server: no direct route on this network');
  });
});
