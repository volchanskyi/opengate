import { useState } from 'react';
import { Link, Navigate, useNavigate } from 'react-router';
import { useAuthStore } from '../../state/auth-store';
import { fireAndForget } from '../../lib/fire-and-forget';
import { AuthForm } from './AuthForm';

export function RegisterPage() {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [displayName, setDisplayName] = useState('');
  const register = useAuthStore((s) => s.register);
  const error = useAuthStore((s) => s.error);
  const isLoading = useAuthStore((s) => s.isLoading);
  const token = useAuthStore((s) => s.token);
  const user = useAuthStore((s) => s.user);
  const navigate = useNavigate();

  if (token && user) {
    return <Navigate to="/devices" replace />;
  }

  const handleSubmit = async () => {
    await register(email, password, displayName);
    if (useAuthStore.getState().token) {
      fireAndForget(navigate('/devices'));
    }
  };

  return (
    <AuthForm
      title="Register"
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
          id: 'displayName',
          label: 'Display Name',
          type: 'text',
          autoComplete: 'name',
          value: displayName,
          onChange: setDisplayName,
        },
        {
          id: 'password',
          label: 'Password',
          type: 'password',
          autoComplete: 'new-password',
          value: password,
          onChange: setPassword,
          required: true,
        },
      ]}
      error={error}
      isLoading={isLoading}
      submitLabel="Register"
      loadingLabel="Registering..."
      onSubmit={handleSubmit}
      footer={
        <>
          Already have an account?{' '}
          <Link to="/login" className="text-blue-400 hover:underline">Login</Link>
        </>
      }
    />
  );
}
