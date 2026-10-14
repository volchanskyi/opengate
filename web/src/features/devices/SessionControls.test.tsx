import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useSessionStore } from '../session';
import { SessionControls } from './SessionControls';

const ALICE = { holder_id: 'u-alice', holder_name: 'Alice', started_at: '2026-10-12T14:02:00Z' };

function presence(deviceId: string) {
  localStorage.setItem(`opengate-session-tab:${deviceId}`, String(Date.now()));
}

describe('SessionControls', () => {
  const endDeviceSession = vi.fn();
  const onEnded = vi.fn();

  beforeEach(() => {
    endDeviceSession.mockReset().mockResolvedValue(true);
    onEnded.mockReset();
    useSessionStore.setState({ endDeviceSession });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
  });

  it('opens a free host’s session tab at once, in the click', async () => {
    const user = userEvent.setup();
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    render(<SessionControls deviceId="dev-1" userId="u-bob" isAdmin={false} onEnded={onEnded} />);

    await user.click(screen.getByRole('button', { name: 'Open session' }));
    expect(open).toHaveBeenCalledWith('/devices/dev-1/session', 'opengate-session-dev-1');
    expect(screen.queryByText(/Session open/)).not.toBeInTheDocument();
  });

  describe('a host another technician holds', () => {
    it('names the holder and leaves a technician only the note that an administrator can end it', () => {
      render(<SessionControls deviceId="dev-1" session={ALICE} userId="u-bob" isAdmin={false} onEnded={onEnded} />);
      expect(screen.getByText(/^Session open — Alice, since \d{1,2}:\d{2}/)).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Open session' })).toBeDisabled();
      expect(screen.queryByRole('button', { name: 'End session' })).not.toBeInTheDocument();
      expect(screen.getByText('an administrator can end it')).toBeInTheDocument();
    });

    it('lets an administrator end it after confirming', async () => {
      const user = userEvent.setup();
      render(<SessionControls deviceId="dev-1" session={ALICE} userId="u-carol" isAdmin onEnded={onEnded} />);

      await user.click(screen.getByRole('button', { name: 'End session' }));
      expect(endDeviceSession).not.toHaveBeenCalled();
      await user.click(screen.getByRole('button', { name: 'Confirm: end Alice’s session' }));

      expect(endDeviceSession).toHaveBeenCalledWith('dev-1');
      expect(onEnded).toHaveBeenCalled();
      expect(screen.queryByText('an administrator can end it')).not.toBeInTheDocument();
    });
  });

  describe('a host this technician holds', () => {
    const MINE = { ...ALICE, holder_id: 'u-alice' };

    it('brings this browser’s session tab forward without reloading it', async () => {
      const user = userEvent.setup();
      presence('dev-1');
      const tab = { location: { href: 'https://app.test/devices/dev-1/session' }, focus: vi.fn(), close: vi.fn() };
      const open = vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window);
      render(<SessionControls deviceId="dev-1" session={MINE} userId="u-alice" isAdmin={false} onEnded={onEnded} />);

      vi.spyOn(document, 'hasFocus').mockReturnValue(false);

      await user.click(screen.getByRole('button', { name: 'Go to session' }));
      await act(() => new Promise((resolve) => setTimeout(resolve, 600)));
      expect(open).toHaveBeenCalledWith('', 'opengate-session-dev-1');
      expect(tab.focus).toHaveBeenCalled();
      expect(screen.queryByText('Open in another tab of this browser')).not.toBeInTheDocument();
    });

    it('asks to be opened from the other tab when the tab stays behind this page', async () => {
      const user = userEvent.setup();
      presence('dev-1');
      const tab = { location: { href: 'https://app.test/devices/dev-1/session' }, focus: vi.fn(), close: vi.fn() };
      vi.spyOn(window, 'open').mockReturnValue(tab as unknown as Window);
      render(<SessionControls deviceId="dev-1" session={MINE} userId="u-alice" isAdmin={false} onEnded={onEnded} />);

      vi.spyOn(document, 'hasFocus').mockReturnValue(true);

      await user.click(screen.getByRole('button', { name: 'Go to session' }));
      expect(await screen.findByText('Open in another tab of this browser')).toBeInTheDocument();
    });

    it('asks to be opened from the other tab when this browser will not bring it forward', async () => {
      const user = userEvent.setup();
      presence('dev-1');
      vi.spyOn(window, 'open').mockReturnValue({ location: { href: 'about:blank' }, focus: vi.fn(), close: vi.fn() } as unknown as Window);
      render(<SessionControls deviceId="dev-1" session={MINE} userId="u-alice" isAdmin={false} onEnded={onEnded} />);

      await user.click(screen.getByRole('button', { name: 'Go to session' }));
      expect(await screen.findByText('Open in another tab of this browser')).toBeInTheDocument();
    });

    it('moves the session here when this browser holds no tab for it', async () => {
      const user = userEvent.setup();
      const open = vi.spyOn(window, 'open').mockReturnValue(null);
      render(<SessionControls deviceId="dev-1" session={MINE} userId="u-alice" isAdmin={false} onEnded={onEnded} />);

      await user.click(screen.getByRole('button', { name: 'Move session here' }));
      expect(open).toHaveBeenCalledWith('/devices/dev-1/session', 'opengate-session-dev-1');
    });
  });
});
