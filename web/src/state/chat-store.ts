import { create } from 'zustand';
import type { ChatMessageFields } from '../lib/protocol/types';

let nextMessageId = 0;

export interface ChatMessage extends ChatMessageFields {
  id: number;
}

interface ChatState {
  messages: ChatMessage[];
  addMessage: (msg: ChatMessageFields) => void;
  clearMessages: () => void;
}

export const useChatStore = create<ChatState>((set) => ({
  messages: [],

  addMessage: (msg) =>
    set((state) => ({ messages: [...state.messages, { ...msg, id: nextMessageId++ }] })),

  clearMessages: () => set({ messages: [] }),
}));
