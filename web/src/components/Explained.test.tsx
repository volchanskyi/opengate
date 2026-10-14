import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Explained } from './Explained';

const TEXT = 'Hosts running this rule now.';

describe('Explained', () => {
  it('names the label by its explanation, for a screen reader', () => {
    render(<Explained label="Watching" explanation={TEXT} />);
    expect(screen.getByText('Watching')).toHaveAccessibleDescription(TEXT);
  });

  it('shows the explanation on hover and hides it when the pointer leaves', async () => {
    const user = userEvent.setup();
    render(<Explained label="Watching" explanation={TEXT} />);
    expect(screen.queryByRole('tooltip')).toBeNull();

    await user.hover(screen.getByText('Watching'));
    expect(screen.getByRole('tooltip')).toHaveTextContent(TEXT);

    await user.unhover(screen.getByText('Watching'));
    expect(screen.queryByRole('tooltip')).toBeNull();
  });

  it('shows the explanation on keyboard focus and hides it on blur', async () => {
    const user = userEvent.setup();
    render(<><Explained label="Watching" explanation={TEXT} /><button type="button">next</button></>);

    await user.tab();
    expect(screen.getByText('Watching')).toHaveFocus();
    expect(screen.getByRole('tooltip')).toHaveTextContent(TEXT);

    await user.tab();
    expect(screen.queryByRole('tooltip')).toBeNull();
  });

  it('hides the explanation on Escape, keeping focus on the label', async () => {
    const user = userEvent.setup();
    render(<Explained label="Watching" explanation={TEXT} />);
    await user.tab();
    expect(screen.getByRole('tooltip')).toBeVisible();

    await user.keyboard('{Escape}');
    expect(screen.queryByRole('tooltip')).toBeNull();
    expect(screen.getByText('Watching')).toHaveFocus();
  });
});
