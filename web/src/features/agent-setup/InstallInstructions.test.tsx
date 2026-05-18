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

  it('selected platform button uses blue-600 active class', () => {
    render(<InstallInstructions manifests={manifests} />);
    const amd64Btn = screen.getByRole('button', { name: 'Linux x86_64' });
    expect(amd64Btn.className).toContain('bg-blue-600');
    expect(amd64Btn.className).not.toContain('bg-gray-800');

    const arm64Btn = screen.getByRole('button', { name: 'Linux ARM64' });
    expect(arm64Btn.className).toContain('bg-gray-800');
    expect(arm64Btn.className).not.toContain('bg-blue-600');
  });

  it('selected button updates classes after switch', () => {
    render(<InstallInstructions manifests={manifests} />);
    fireEvent.click(screen.getByText('Linux ARM64'));
    const arm64Btn = screen.getByRole('button', { name: 'Linux ARM64' });
    expect(arm64Btn.className).toContain('bg-blue-600');
    const amd64Btn = screen.getByRole('button', { name: 'Linux x86_64' });
    expect(amd64Btn.className).toContain('bg-gray-800');
  });

  it('Download binary anchor has target=_blank and rel=noopener noreferrer for security', () => {
    render(<InstallInstructions manifests={manifests} />);
    const link = screen.getByRole('link', { name: /download binary/i });
    expect(link.getAttribute('target')).toBe('_blank');
    expect(link.getAttribute('rel')).toBe('noopener noreferrer');
  });

  it('Manual install arrow has rotate-90 class when expanded', () => {
    render(<InstallInstructions manifests={manifests} />);
    const toggleBtn = screen.getByRole('button', { name: /Manual Install/ });
    const arrow = toggleBtn.querySelector('span');
    expect(arrow?.className).not.toContain('rotate-90');

    fireEvent.click(toggleBtn);
    const arrowAfter = toggleBtn.querySelector('span');
    expect(arrowAfter?.className).toContain('rotate-90');
  });

  it('Manual install hides on second click', () => {
    render(<InstallInstructions manifests={manifests} />);
    const btn = screen.getByRole('button', { name: /Manual Install/ });
    fireEvent.click(btn);
    expect(screen.getByText(/download the agent binary/i)).toBeInTheDocument();
    fireEvent.click(btn);
    expect(screen.queryByText(/download the agent binary/i)).not.toBeInTheDocument();
  });

  it('install script section text matches the literal label', () => {
    render(<InstallInstructions manifests={manifests} />);
    expect(screen.getByText('Download install.sh')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Download install.sh' })).toHaveAttribute('href', '/api/v1/server/install.sh');
  });

  it('manual install no-manifest line names the current platform', () => {
    render(<InstallInstructions manifests={[]} />);
    fireEvent.click(screen.getByText('Manual Install'));
    expect(screen.getByText(/No binary available for linux\/amd64\./)).toBeInTheDocument();
  });
});
