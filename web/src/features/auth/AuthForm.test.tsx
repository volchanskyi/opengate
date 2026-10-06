import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi } from 'vitest';
import { AuthForm, type AuthField } from './AuthForm';

function makeFields(onEmail = vi.fn(), onName = vi.fn()): AuthField[] {
  return [
    {
      id: 'email',
      label: 'Email',
      type: 'email',
      autoComplete: 'username',
      value: 'a@b.c',
      onChange: onEmail,
      required: true,
    },
    {
      id: 'displayName',
      label: 'Display Name',
      type: 'text',
      autoComplete: 'name',
      value: '',
      onChange: onName,
    },
  ];
}

function renderForm(overrides: Partial<React.ComponentProps<typeof AuthForm>> = {}) {
  return render(
    <AuthForm
      title="Sign"
      fields={makeFields()}
      error={null}
      isLoading={false}
      submitLabel="Go"
      loadingLabel="Going..."
      onSubmit={vi.fn()}
      footer={<span>footer text</span>}
      {...overrides}
    />,
  );
}

describe('AuthForm', () => {
  it('renders the heading, footer and a labelled input per field', () => {
    renderForm();
    expect(screen.getByRole('heading', { name: 'Sign' })).toBeInTheDocument();
    expect(screen.getByLabelText('Email')).toHaveAttribute('type', 'email');
    expect(screen.getByLabelText('Email')).toHaveAttribute('autocomplete', 'username');
    expect(screen.getByLabelText('Email')).toBeRequired();
    expect(screen.getByLabelText('Email')).toHaveValue('a@b.c');
    expect(screen.getByLabelText('Display Name')).not.toBeRequired();
    expect(screen.getByLabelText('Display Name')).toHaveAttribute('id', 'displayName');
    expect(screen.getByText('footer text')).toBeInTheDocument();
  });

  it('calls a field onChange with the typed text', async () => {
    const onName = vi.fn();
    renderForm({ fields: makeFields(vi.fn(), onName) });
    await userEvent.type(screen.getByLabelText('Display Name'), 'z');
    expect(onName).toHaveBeenCalledWith('z');
  });

  it('shows the error line only when an error is set', () => {
    const { rerender } = renderForm();
    expect(screen.queryByText('bad credentials')).toBeNull();
    rerender(
      <AuthForm
        title="Sign"
        fields={makeFields()}
        error="bad credentials"
        isLoading={false}
        submitLabel="Go"
        loadingLabel="Going..."
        onSubmit={vi.fn()}
        footer={null}
      />,
    );
    expect(screen.getByText('bad credentials')).toBeInTheDocument();
  });

  it('switches the submit button to its loading label and disables it', () => {
    renderForm({ isLoading: true });
    const button = screen.getByRole('button', { name: 'Going...' });
    expect(button).toBeDisabled();
  });

  it('submits through onSubmit and cancels the browser submit', () => {
    const onSubmit = vi.fn();
    const { container } = renderForm({ onSubmit });
    expect(fireEvent.submit(container.querySelector('form')!)).toBe(false);
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });
});
