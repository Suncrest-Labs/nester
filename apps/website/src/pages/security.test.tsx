import React from 'react';
import { render, screen } from '@testing-library/react';
import SecurityPage from './security';

describe('SecurityPage', () => {
  it('renders without errors', () => {
    render(<SecurityPage />);
    expect(screen.getByText('Security & Audit')).toBeInTheDocument();
  });

  it('displays audit status as Pending', () => {
    render(<SecurityPage />);
    expect(screen.getByText('Pending')).toBeInTheDocument();
  });

  it('lists all contracts in scope', () => {
    render(<SecurityPage />);
    const contracts = [
      'vault',
      'vault_token',
      'allocation_strategy',
      'yield_registry',
      'nester',
      'treasury',
      'timelock',
    ];
    contracts.forEach((contract) => {
      expect(screen.getByText(contract)).toBeInTheDocument();
    });
  });

  it('links to the threat model document', () => {
    render(<SecurityPage />);
    const link = screen.getByText('threat model document');
    expect(link).toBeInTheDocument();
    expect(link.closest('a')).toHaveAttribute('href', '/AUDIT_THREAT_MODEL.md');
  });

  it('has a bug bounty section with contact email', () => {
    render(<SecurityPage />);
    expect(screen.getByText('Bug Bounty')).toBeInTheDocument();
    const emailLink = screen.getByText('security@nester.finance');
    expect(emailLink).toBeInTheDocument();
    expect(emailLink.closest('a')).toHaveAttribute(
      'href',
      'mailto:security@nester.finance'
    );
  });
});
