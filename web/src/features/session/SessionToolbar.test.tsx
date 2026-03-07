import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi } from 'vitest';
import { SessionToolbar } from './SessionToolbar';

describe('SessionToolbar', () => {
  it.each([
    ['connecting', 'Connecting...'],
    ['connected', 'Connected'],
    ['disconnected', 'Disconnected'],
    ['error', 'Error'],
  ] as const)('shows "%s" label for %s state', (state, expected) => {
    render(<SessionToolbar connectionState={state} onDisconnect={vi.fn()} />);
    expect(screen.getByText(expected)).toBeInTheDocument();
  });

  it('calls onDisconnect when disconnect button is clicked', async () => {
    const user = userEvent.setup();
    const onDisconnect = vi.fn();
    render(<SessionToolbar connectionState="connected" onDisconnect={onDisconnect} />);

    await user.click(screen.getByText('Disconnect'));
    expect(onDisconnect).toHaveBeenCalledOnce();
  });

  it('renders disconnect button', () => {
    render(<SessionToolbar connectionState="connected" onDisconnect={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Disconnect' })).toBeInTheDocument();
  });
});
