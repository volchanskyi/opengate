import { render, screen } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { AmtBadge } from './AmtBadge';

describe('AmtBadge', () => {
  it('renders the Intel AMT label for a capable device', () => {
    render(<AmtBadge amt={{ available: true }} />);
    expect(screen.getByText('Intel AMT')).toBeInTheDocument();
  });

  it('renders nothing when the device carries no AMT property', () => {
    const { container } = render(<AmtBadge amt={undefined} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing when AMT is not available and nothing is linked', () => {
    const { container } = render(<AmtBadge amt={{ available: false }} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('shows on capability alone, before any connection has dialled in', () => {
    render(<AmtBadge amt={{ available: true }} />);
    expect(screen.getByText('Intel AMT').getAttribute('title')).toBe('Intel AMT · not connected');
  });

  it('reports an online CIRA connection in the tooltip', () => {
    render(<AmtBadge amt={{ available: true, status: 'online', uuid: 'a-uuid' }} />);
    expect(screen.getByText('Intel AMT').getAttribute('title')).toBe('Intel AMT · online');
  });

  it('reports an offline CIRA connection in the tooltip', () => {
    render(<AmtBadge amt={{ available: true, status: 'offline', uuid: 'a-uuid' }} />);
    expect(screen.getByText('Intel AMT').getAttribute('title')).toBe('Intel AMT · offline');
  });

  it('still renders for a linked device whose agent has not reported capability', () => {
    // The connection is proof enough; the badge must not disappear while the
    // agent's next hardware report is outstanding.
    render(<AmtBadge amt={{ available: false, status: 'online', uuid: 'a-uuid' }} />);
    expect(screen.getByText('Intel AMT').getAttribute('title')).toBe('Intel AMT · online');
  });

  it('uses the blue styling that distinguishes it from status and maintenance', () => {
    render(<AmtBadge amt={{ available: true }} />);
    expect(screen.getByText('Intel AMT')).toHaveClass('text-blue-300');
  });

  it('appends a caller-supplied className last', () => {
    render(<AmtBadge amt={{ available: true }} className="ml-2" />);
    expect(screen.getByText('Intel AMT').getAttribute('class')).toContain('border-blue-700 ml-2');
  });
});
