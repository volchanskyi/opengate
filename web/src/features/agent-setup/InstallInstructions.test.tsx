import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { InstallInstructions } from './InstallInstructions';

const manifests = [
  { version: '1.0.0', os: 'linux', arch: 'amd64', url: 'https://example.com/agent-amd64', sha256: 'abc', signature: 'sig', created_at: '' },
  { version: '1.0.0', os: 'linux', arch: 'arm64', url: 'https://example.com/agent-arm64', sha256: 'def', signature: 'sig2', created_at: '' },
];

describe('InstallInstructions', () => {
  it('renders platform selector with default amd64', () => {
    render(<InstallInstructions manifests={manifests} />);
    expect(screen.getByText('Linux x86_64')).toBeInTheDocument();
    expect(screen.getByText('Linux ARM64')).toBeInTheDocument();
    expect(screen.getByText('1.0.0')).toBeInTheDocument();
  });

  it('switches platform when ARM64 is clicked', () => {
    render(<InstallInstructions manifests={manifests} />);
    fireEvent.click(screen.getByText('Linux ARM64'));
    expect(screen.getByRole('link', { name: /download binary/i })).toHaveAttribute('href', 'https://example.com/agent-arm64');
  });

  it('shows missing message when no manifest for platform', () => {
    render(<InstallInstructions manifests={[]} />);
    expect(screen.getByText(/no agent binaries published/i)).toBeInTheDocument();
  });

  it('renders install script download link', () => {
    render(<InstallInstructions manifests={manifests} />);
    expect(screen.getByText('Download install.sh')).toBeInTheDocument();
  });

  it('toggles manual install section', () => {
    render(<InstallInstructions manifests={manifests} />);
    expect(screen.queryByText(/download the agent binary/i)).not.toBeInTheDocument();
    fireEvent.click(screen.getByText('Manual Install'));
    expect(screen.getByText(/download the agent binary/i)).toBeInTheDocument();
  });
});
