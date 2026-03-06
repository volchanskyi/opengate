import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { StatusBadge } from './StatusBadge';

describe('StatusBadge', () => {
  it('renders Online with green dot', () => {
    render(<StatusBadge status="online" />);
    expect(screen.getByText('Online')).toBeInTheDocument();
  });

  it('renders Offline with gray dot', () => {
    render(<StatusBadge status="offline" />);
    expect(screen.getByText('Offline')).toBeInTheDocument();
  });

  it('renders Connecting with yellow dot', () => {
    render(<StatusBadge status="connecting" />);
    expect(screen.getByText('Connecting')).toBeInTheDocument();
  });
});
