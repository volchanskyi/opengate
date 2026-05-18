import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useConnectionStore } from '../../state/connection-store';
import { useChatStore } from '../../state/chat-store';
import { MessengerView } from './MessengerView';

vi.mock('../../lib/api', () => ({
  api: {
    GET: vi.fn().mockResolvedValue({ data: undefined, error: { error: 'mock' }, response: { status: 404 } }),
    POST: vi.fn(),
    DELETE: vi.fn(),
  },
}));

describe('MessengerView', () => {
  const mockSendControl = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    useChatStore.setState({ messages: [] });
    useConnectionStore.setState({
      state: 'connected',
      transport: { sendControl: mockSendControl } as never,
    });
  });

  it('renders input field and send button', () => {
    render(<MessengerView />);
    expect(screen.getByPlaceholderText(/type a message/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /send/i })).toBeInTheDocument();
  });

  it('renders messages from store', () => {
    useChatStore.setState({
      messages: [
        { text: 'Hello', sender: 'browser', id: '1' },
        { text: 'Hi there', sender: 'agent', id: '2' },
      ],
    });
    render(<MessengerView />);
    expect(screen.getByText('Hello')).toBeInTheDocument();
    expect(screen.getByText('Hi there')).toBeInTheDocument();
  });

  it('sends message on button click', async () => {
    const user = userEvent.setup();
    render(<MessengerView />);

    const input = screen.getByPlaceholderText(/type a message/i);
    await user.type(input, 'test message');
    await user.click(screen.getByRole('button', { name: /send/i }));

    expect(mockSendControl).toHaveBeenCalledWith({
      type: 'ChatMessage',
      text: 'test message',
      sender: 'browser',
    });
    // Message should be added to store
    expect(useChatStore.getState().messages).toContainEqual(
      expect.objectContaining({ text: 'test message', sender: 'browser' }),
    );
  });

  it('sends message on Enter key', async () => {
    const user = userEvent.setup();
    render(<MessengerView />);

    const input = screen.getByPlaceholderText(/type a message/i);
    await user.type(input, 'enter test{Enter}');

    expect(mockSendControl).toHaveBeenCalledWith({
      type: 'ChatMessage',
      text: 'enter test',
      sender: 'browser',
    });
  });

  it('clears input after sending', async () => {
    const user = userEvent.setup();
    render(<MessengerView />);

    const input = screen.getByPlaceholderText(/type a message/i);
    await user.type(input, 'test{Enter}');

    expect(input).toHaveValue('');
  });

  it('does not send empty messages', async () => {
    const user = userEvent.setup();
    render(<MessengerView />);

    await user.click(screen.getByRole('button', { name: /send/i }));
    expect(mockSendControl).not.toHaveBeenCalled();
  });

  it('shows placeholder when disconnected', () => {
    useConnectionStore.setState({ state: 'disconnected' });
    render(<MessengerView />);
    expect(screen.getByText(/waiting for connection/i)).toBeInTheDocument();
  });

  it('input.trim() suppresses whitespace-only sends', async () => {
    const user = userEvent.setup();
    render(<MessengerView />);
    const input = screen.getByPlaceholderText(/type a message/i);
    await user.type(input, '   ');
    await user.click(screen.getByRole('button', { name: /send/i }));
    expect(mockSendControl).not.toHaveBeenCalled();
  });

  it('trimmed value (not the raw input) is sent over transport', async () => {
    const user = userEvent.setup();
    render(<MessengerView />);
    const input = screen.getByPlaceholderText(/type a message/i);
    await user.type(input, '   hello   ');
    await user.click(screen.getByRole('button', { name: /send/i }));
    expect(mockSendControl).toHaveBeenCalledWith({
      type: 'ChatMessage',
      text: 'hello',
      sender: 'browser',
    });
  });

  it('Shift+Enter inserts a newline instead of sending', async () => {
    const user = userEvent.setup();
    render(<MessengerView />);
    const input = screen.getByPlaceholderText(/type a message/i);
    await user.type(input, 'line1{Shift>}{Enter}{/Shift}line2');
    // sendMessage must not fire on shift+enter.
    expect(mockSendControl).not.toHaveBeenCalled();
  });

  it('does not send while disconnected even on Enter', async () => {
    const user = userEvent.setup();
    // sendMessage early-returns when !transport. Set transport to null.
    useConnectionStore.setState({ state: 'connected', transport: null });
    render(<MessengerView />);
    const input = screen.getByPlaceholderText(/type a message/i);
    await user.type(input, 'attempt{Enter}');
    expect(mockSendControl).not.toHaveBeenCalled();
  });

  it('browser-side message bubbles use the blue class; agent-side uses gray', () => {
    useChatStore.setState({
      messages: [
        { text: 'me', sender: 'browser', id: '1' },
        { text: 'them', sender: 'agent', id: '2' },
      ],
    });
    render(<MessengerView />);
    const me = screen.getByText('me');
    const them = screen.getByText('them');
    expect(me.className).toContain('bg-blue-600');
    expect(me.className).toContain('ml-auto');
    expect(them.className).toContain('bg-gray-700');
    expect(them.className).not.toContain('bg-blue-600');
    expect(them.className).not.toContain('ml-auto');
  });

  it('subscribes to onControlMessage and adds ChatMessage payloads to chat store', () => {
    let captured: ((msg: { type: string; text?: string; sender?: string }) => void) | null = null;
    const setOnControlMessage = vi.fn((cb) => { captured = cb; });
    useConnectionStore.setState({
      state: 'connected',
      transport: { sendControl: mockSendControl } as never,
      setOnControlMessage,
    });
    render(<MessengerView />);
    expect(setOnControlMessage).toHaveBeenCalled();

    captured!({ type: 'ChatMessage', text: 'inbound', sender: 'agent' });
    expect(useChatStore.getState().messages).toContainEqual(
      expect.objectContaining({ text: 'inbound', sender: 'agent' }),
    );
  });

  it('ignores non-ChatMessage payloads from the control channel', () => {
    let captured: ((msg: { type: string; text?: string; sender?: string }) => void) | null = null;
    useConnectionStore.setState({
      state: 'connected',
      transport: { sendControl: mockSendControl } as never,
      setOnControlMessage: ((cb: never) => { captured = cb; }) as never,
    });
    render(<MessengerView />);
    captured!({ type: 'SomethingElse', text: 'not chat' });
    expect(useChatStore.getState().messages).toEqual([]);
  });

  it('unmount clears the control-message handler', () => {
    const setOnControlMessage = vi.fn();
    useConnectionStore.setState({
      state: 'connected',
      transport: { sendControl: mockSendControl } as never,
      setOnControlMessage,
    });
    const { unmount } = render(<MessengerView />);
    setOnControlMessage.mockClear();
    unmount();
    // Cleanup invokes setOnControlMessage(null).
    expect(setOnControlMessage).toHaveBeenCalledWith(null);
  });

  it('does not wire onControlMessage when transport is null', () => {
    const setOnControlMessage = vi.fn();
    useConnectionStore.setState({
      state: 'connected',
      transport: null,
      setOnControlMessage,
    });
    render(<MessengerView />);
    expect(setOnControlMessage).not.toHaveBeenCalled();
  });
});
