import { Component, type ErrorInfo, type ReactNode } from 'react';
import { reportClientError } from '../lib/report-error';

interface Props {
  children: ReactNode;
}

interface State {
  hasError: boolean;
}

export class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false };

  static getDerivedStateFromError(): State {
    return { hasError: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('ErrorBoundary caught:', error, info.componentStack);
    reportClientError({
      message: error.message,
      source: 'ErrorBoundary',
      stack: error.stack ?? info.componentStack ?? undefined,
    });
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="min-h-screen bg-gray-900 text-white flex items-center justify-center">
          <div className="text-center space-y-4">
            <h1 className="text-2xl font-bold">Something went wrong</h1>
            <p className="text-gray-400">An unexpected error occurred.</p>
            <button
              type="button"
              onClick={() => globalThis.location.reload()}
              className="px-4 py-2 bg-blue-600 hover:bg-blue-700 rounded font-medium"
            >
              Reload page
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
