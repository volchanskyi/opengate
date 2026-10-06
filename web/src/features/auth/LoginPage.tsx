import { useState, useEffect } from 'react';
import { Link, Navigate, useNavigate } from 'react-router';
import { useAuthStore } from '../../state/auth-store';
import { fireAndForget } from '../../lib/fire-and-forget';
import { AuthForm } from './AuthForm';

export function LoginPage() {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const login = useAuthStore((s) => s.login);
  const error = useAuthStore((s) => s.error);
  const isLoading = useAuthStore((s) => s.isLoading);
  const token = useAuthStore((s) => s.token);
  const user = useAuthStore((s) => s.user);
  const navigate = useNavigate();

  useEffect(() => {
    if (token && user) {
      fireAndForget(navigate('/devices', { replace: true }));
    }
  }, [token, user, navigate]);

  if (token && user) {
    return <Navigate to="/devices" replace />;
  }

  return (
    <AuthForm
      title="Login"
      fields={[
        {
          id: 'email',
          label: 'Email',
          type: 'email',
          autoComplete: 'username',
          value: email,
          onChange: setEmail,
          required: true,
        },
        {
          id: 'password',
          label: 'Password',
          type: 'password',
          autoComplete: 'current-password',
          value: password,
          onChange: setPassword,
          required: true,
        },
      ]}
      error={error}
      isLoading={isLoading}
      submitLabel="Login"
      loadingLabel="Logging in..."
      onSubmit={() => login(email, password)}
      footer={
        <>
          Don&apos;t have an account?{' '}
          <Link to="/register" className="text-blue-400 hover:underline">Register</Link>
        </>
      }
    />
  );
}
