import { create } from 'zustand';
import type { ChatMessageFields } from '../lib/protocol/types';

type ChatMessage = ChatMessageFields;

interface ChatState {
  messages: ChatMessage[];
  addMessage: (msg: ChatMessage) => void;
  clearMessages: () => void;
}

export const useChatStore = create<ChatState>((set) => ({
  messages: [],

  addMessage: (msg) =>
    set((state) => ({ messages: [...state.messages, msg] })),

  clearMessages: () => set({ messages: [] }),
}));
