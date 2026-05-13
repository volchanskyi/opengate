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
    const { container } = render(<SessionToolbar connectionState={state} onDisconnect={vi.fn()} />);
    expect(screen.getByText(expected)).toBeInTheDocument();
    // Pin the color class on the indicator dot — kills StringLiteral mutants
    // on each color value (`'bg-yellow-500'` → `""`).
    const dot = container.querySelector('.rounded-full');
    expect(dot?.className).toContain(expectedColor);
  });

  it('shows "Unknown" label for an unrecognized state value', () => {
    // Cast through unknown to bypass typing — the switch's `default` arm must
    // hit, kills the `'Unknown'` → `""` and `'bg-gray-500'` → `""` mutants
    // that NoCoverage flagged.
    const { container } = render(
      <SessionToolbar connectionState={'totally-bogus' as unknown as 'connected'} onDisconnect={vi.fn()} />,
    );
    expect(screen.getByText('Unknown')).toBeInTheDocument();
    expect(container.querySelector('.rounded-full')?.className).toContain('bg-gray-500');
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
