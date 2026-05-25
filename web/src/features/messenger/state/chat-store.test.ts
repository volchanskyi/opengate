import { describe, it, expect, beforeEach } from 'vitest';
import { useChatStore } from './chat-store';

describe('chat-store', () => {
  beforeEach(() => {
    useChatStore.setState({ messages: [] });
  });

  it('has empty initial messages', () => {
    expect(useChatStore.getState().messages).toEqual([]);
  });

  it('addMessage appends a message', () => {
    const { addMessage } = useChatStore.getState();
    addMessage({ text: 'hello', sender: 'browser' });

    const { messages } = useChatStore.getState();
    expect(messages).toHaveLength(1);
    expect(messages[0]).toEqual(expect.objectContaining({ text: 'hello', sender: 'browser' }));
  });

  it('addMessage preserves existing messages', () => {
    const { addMessage } = useChatStore.getState();
    addMessage({ text: 'first', sender: 'agent' });
    addMessage({ text: 'second', sender: 'browser' });

    const { messages } = useChatStore.getState();
    expect(messages).toHaveLength(2);
    expect(messages[0]!.text).toBe('first');
    expect(messages[1]!.text).toBe('second');
  });

  it('addMessage assigns unique ids', () => {
    const { addMessage } = useChatStore.getState();
    addMessage({ text: 'a', sender: 'browser' });
    addMessage({ text: 'b', sender: 'agent' });

    const { messages } = useChatStore.getState();
    expect(messages[0]!.id).toBeDefined();
    expect(messages[1]!.id).toBeDefined();
    expect(messages[0]!.id).not.toEqual(messages[1]!.id);
  });

  it('clearMessages resets to empty', () => {
    const { addMessage, clearMessages } = useChatStore.getState();
    addMessage({ text: 'hello', sender: 'browser' });
    clearMessages();

    expect(useChatStore.getState().messages).toEqual([]);
  });
});
