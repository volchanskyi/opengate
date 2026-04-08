import { useEffect } from 'react';
import { RouterProvider } from 'react-router-dom';
import { router } from './router';
import { useAuthStore } from './state/auth-store';
import { ErrorBoundary } from './components/ErrorBoundary';
import { fireAndForget } from './lib/fire-and-forget';

function App() {
  const hydrate = useAuthStore((s) => s.hydrate);
  const hydrated = useAuthStore((s) => s.hydrated);

  useEffect(() => {
    fireAndForget(hydrate());
  }, [hydrate]);

  if (!hydrated) return null;

  return (
    <ErrorBoundary>
      <RouterProvider router={router} />
    </ErrorBoundary>
  );
}

export default App;
