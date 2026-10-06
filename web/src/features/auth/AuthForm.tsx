import type { ReactNode } from 'react';
import { fireAndForget } from '../../lib/fire-and-forget';

/** One labelled input of an {@link AuthForm}. */
export interface AuthField {
  id: string;
  label: string;
  type: 'email' | 'password' | 'text';
  autoComplete: string;
  value: string;
  onChange: (value: string) => void;
  required?: boolean;
}

interface AuthFormProps {
  title: string;
  fields: AuthField[];
  error: string | null;
  isLoading: boolean;
  submitLabel: string;
  loadingLabel: string;
  onSubmit: () => Promise<void>;
  footer: ReactNode;
}

const INPUT_CLASS = 'w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded text-white';

/** The card shared by the login and register pages: heading, fields, error line, submit, footer. */
export function AuthForm({
  title,
  fields,
  error,
  isLoading,
  submitLabel,
  loadingLabel,
  onSubmit,
  footer,
}: Readonly<AuthFormProps>) {
  return (
    <div className="min-h-screen bg-gray-900 text-white flex items-center justify-center">
      <div className="w-full max-w-sm p-6">
        <h1 className="text-2xl font-bold mb-6 text-center">{title}</h1>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            fireAndForget(onSubmit());
          }}
          className="space-y-4"
        >
          {fields.map((field) => (
            <div key={field.id}>
              <label htmlFor={field.id} className="block text-sm mb-1">{field.label}</label>
              <input
                id={field.id}
                type={field.type}
                autoComplete={field.autoComplete}
                value={field.value}
                onChange={(e) => field.onChange(e.target.value)}
                className={INPUT_CLASS}
                required={field.required}
              />
            </div>
          ))}
          {error && <p className="text-red-400 text-sm">{error}</p>}
          <button
            type="submit"
            disabled={isLoading}
            className="w-full py-2 bg-blue-600 hover:bg-blue-700 rounded font-medium disabled:opacity-50"
          >
            {isLoading ? loadingLabel : submitLabel}
          </button>
        </form>
        <p className="mt-4 text-center text-sm text-gray-400">{footer}</p>
      </div>
    </div>
  );
}
