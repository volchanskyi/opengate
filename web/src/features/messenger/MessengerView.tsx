import { useState, useEffect, useRef } from 'react';
import { useConnectionStore } from '../../state/connection-store';
import { useChatStore } from '../../state/chat-store';

export function MessengerView() {
  const connectionState = useConnectionStore((s) => s.state);
  const transport = useConnectionStore((s) => s.transport);
  const setOnControlMessage = useConnectionStore((s) => s.setOnControlMessage);
  const messages = useChatStore((s) => s.messages);
  const addMessage = useChatStore((s) => s.addMessage);
  const [input, setInput] = useState('');
  const bottomRef = useRef<HTMLDivElement>(null);

  // Listen for incoming ChatMessage control messages
  useEffect(() => {
    if (!transport) return;
    setOnControlMessage((msg) => {
      if (msg.type === 'ChatMessage') {
        addMessage({ text: msg.text, sender: msg.sender });
      }
    });
    return () => setOnControlMessage(null);
  }, [transport, setOnControlMessage, addMessage]);

  // Auto-scroll on new messages
  useEffect(() => {
    bottomRef.current?.scrollIntoView?.({ behavior: 'smooth' });
  }, [messages]);

  if (connectionState !== 'connected') {
    return (
      <div className="flex items-center justify-center h-full">
        <p className="text-gray-400">Waiting for connection...</p>
      </div>
    );
  }

  const sendMessage = () => {
    const text = input.trim();
    if (!text || !transport) return;
    const msg = { type: 'ChatMessage' as const, text, sender: 'browser' };
    transport.sendControl(msg);
    addMessage({ text, sender: 'browser' });
    setInput('');
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendMessage();
    }
  };

  return (
    <div className="flex flex-col h-full p-4">
      <div className="flex-1 overflow-y-auto space-y-2 mb-4">
        {messages.map((msg) => (
          <div
            key={msg.id}
            className={`max-w-[80%] px-3 py-2 rounded-lg text-sm ${
              msg.sender === 'browser'
                ? 'ml-auto bg-blue-600 text-white'
                : 'bg-gray-700 text-gray-200'
            }`}
          >
            {msg.text}
          </div>
        ))}
        <div ref={bottomRef} />
      </div>

      <div className="flex gap-2">
        <input
          type="text"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Type a message..."
          className="flex-1 px-3 py-2 bg-gray-800 border border-gray-600 rounded text-sm text-white placeholder-gray-400 focus:outline-none focus:border-blue-500"
        />
        <button
          type="button"
          onClick={sendMessage}
          className="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded text-sm font-medium"
        >
          Send
        </button>
      </div>
    </div>
  );
}
