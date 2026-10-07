import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi } from 'vitest';
import { EnrollmentTokenForm } from './EnrollmentTokenForm';

function renderForm(onCreateToken = vi.fn().mockResolvedValue(undefined)) {
  render(
    <EnrollmentTokenForm
      enrollmentTokens={[]}
      showTokenForm
      setShowTokenForm={vi.fn()}
      copiedField={null}
      onCopy={vi.fn()}
      onCreateToken={onCreateToken}
      onDeleteToken={vi.fn().mockResolvedValue(undefined)}
    />,
  );
  return onCreateToken;
}

describe('EnrollmentTokenForm', () => {
  it('creates a token with what was typed', async () => {
    const onCreateToken = renderForm();

    await userEvent.type(screen.getByLabelText('Label'), 'Branch office');
    fireEvent.change(screen.getByLabelText('Max uses (0 = unlimited)'), { target: { value: '5' } });
    fireEvent.change(screen.getByLabelText('Expires in (hours)'), { target: { value: '48' } });
    await userEvent.click(screen.getByText('Create'));

    expect(onCreateToken).toHaveBeenCalledWith({ label: 'Branch office', max_uses: 5, expires_in_hours: 48 });
  });
  it('after creating a token the uses and hours go back to their defaults', async () => {
    renderForm();

    fireEvent.change(screen.getByLabelText('Max uses (0 = unlimited)'), { target: { value: '5' } });
    fireEvent.change(screen.getByLabelText('Expires in (hours)'), { target: { value: '48' } });
    await userEvent.click(screen.getByText('Create'));

    await waitFor(() => {
      expect(screen.getByLabelText('Max uses (0 = unlimited)')).toHaveValue(0);
    });
    expect(screen.getByLabelText('Expires in (hours)')).toHaveValue(24);
  });
});
