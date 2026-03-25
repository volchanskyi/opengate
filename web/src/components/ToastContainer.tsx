import { useToastStore } from '../state/toast-store';

const typeStyles = {
  success: 'bg-green-900/80 border-green-700 text-green-300',
  error: 'bg-red-900/80 border-red-700 text-red-300',
  info: 'bg-blue-900/80 border-blue-700 text-blue-300',
} as const;

export function ToastContainer() {
  const toasts = useToastStore((s) => s.toasts);
  const removeToast = useToastStore((s) => s.removeToast);

  if (toasts.length === 0) return null;

  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2 max-w-sm" aria-live="polite" aria-label="Notifications">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          role="alert"
          className={`border rounded-lg px-4 py-3 text-sm shadow-lg flex items-start gap-2 ${typeStyles[toast.type]}`}
        >
          <span className="flex-1">{toast.message}</span>
          <button
            type="button"
            onClick={() => removeToast(toast.id)}
            className="opacity-60 hover:opacity-100 text-xs leading-none"
          >
            x
          </button>
        </div>
      ))}
    </div>
  );
}
