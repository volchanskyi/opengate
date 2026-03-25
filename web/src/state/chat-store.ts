import { create } from 'zustand';
import type { ChatMessageFields } from '../lib/protocol/types';

export interface ChatMessage extends ChatMessageFields {
  id: string;
}

interface ChatState {
  messages: ChatMessage[];
  addMessage: (msg: ChatMessageFields) => void;
  clearMessages: () => void;
}

export const useChatStore = create<ChatState>((set) => ({
  messages: [],

  addMessage: (msg) =>
    set((state) => ({ messages: [...state.messages, { ...msg, id: crypto.randomUUID() }] })),

  clearMessages: () => set({ messages: [] }),
}));
