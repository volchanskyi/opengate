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
        { text: 'Hello', sender: 'browser', id: 1 },
        { text: 'Hi there', sender: 'agent', id: 2 },
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
});
