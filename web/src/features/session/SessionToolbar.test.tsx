import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi } from 'vitest';
import { SessionToolbar } from './SessionToolbar';

describe('SessionToolbar', () => {
  it('shows "Connecting..." when state is connecting', () => {
    render(<SessionToolbar connectionState="connecting" onDisconnect={vi.fn()} />);
    expect(screen.getByText('Connecting...')).toBeInTheDocument();
  });

  it('shows "Connected" when state is connected', () => {
    render(<SessionToolbar connectionState="connected" onDisconnect={vi.fn()} />);
    expect(screen.getByText('Connected')).toBeInTheDocument();
  });

  it('shows "Disconnected" when state is disconnected', () => {
    render(<SessionToolbar connectionState="disconnected" onDisconnect={vi.fn()} />);
    expect(screen.getByText('Disconnected')).toBeInTheDocument();
  });

  it('shows "Error" when state is error', () => {
    render(<SessionToolbar connectionState="error" onDisconnect={vi.fn()} />);
    expect(screen.getByText('Error')).toBeInTheDocument();
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
