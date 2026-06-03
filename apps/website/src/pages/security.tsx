import React from 'react';
import Head from 'next/head';
import Link from 'next/link';

const contractsInScope = [
  'vault',
  'vault_token',
  'allocation_strategy',
  'yield_registry',
  'nester',
  'treasury',
  'timelock',
];

export default function SecurityPage() {
  return (
    <>
      <Head>
        <title>Security & Audit | Nester</title>
        <meta
          name="description"
          content="Nester smart contract security audit status, scope, and bug bounty program."
        />
      </Head>
      <main className="min-h-screen bg-gray-50 py-12 px-4 sm:px-6 lg:px-8">
        <div className="max-w-4xl mx-auto">
          <h1 className="text-4xl font-bold text-gray-900 mb-8">
            Security & Audit
          </h1>

          {/* Smart Contract Audit Section */}
          <section className="bg-white shadow rounded-lg p-6 mb-8">
            <h2 className="text-2xl font-semibold text-gray-800 mb-4">
              Smart Contract Audit
            </h2>
            <div className="mb-4">
              <span className="inline-flex items-center px-3 py-1 rounded-full text-sm font-medium bg-yellow-100 text-yellow-800">
                Pending
              </span>
              <p className="mt-2 text-gray-600">
                Audit scheduled — details will be announced once confirmed with
                the auditor.
              </p>
            </div>
            <div className="mb-4">
              <h3 className="text-lg font-medium text-gray-700 mb-2">
                Threat Model
              </h3>
              <p className="text-gray-600">
                Review our{' '}
                <Link
                  href="/AUDIT_THREAT_MODEL.md"
                  className="text-indigo-600 hover:text-indigo-500 underline"
                >
                  threat model document
                </Link>{' '}
                for a detailed analysis of potential risks and mitigations.
              </p>
            </div>
            <div>
              <h3 className="text-lg font-medium text-gray-700 mb-2">
                Contracts in Scope
              </h3>
              <ul className="list-disc list-inside text-gray-600 space-y-1">
                {contractsInScope.map((contract) => (
                  <li key={contract}>{contract}</li>
                ))}
              </ul>
            </div>
          </section>

          {/* Bug Bounty Section */}
          <section className="bg-white shadow rounded-lg p-6">
            <h2 className="text-2xl font-semibold text-gray-800 mb-4">
              Bug Bounty
            </h2>
            <p className="text-gray-600 mb-4">
              We encourage responsible disclosure of security vulnerabilities.
              If you discover a bug or security issue in any of our smart
              contracts or infrastructure, please report it privately.
            </p>
            <div className="mb-4">
              <h3 className="text-lg font-medium text-gray-700 mb-2">
                Disclosure Process
              </h3>
              <ol className="list-decimal list-inside text-gray-600 space-y-1">
                <li>
                  Email your findings to{' '}
                  <a
                    href="mailto:security@nester.finance"
                    className="text-indigo-600 hover:text-indigo-500 underline"
                  >
                    security@nester.finance
                  </a>
                </li>
                <li>
                  Include a detailed description of the vulnerability and steps
                  to reproduce.
                </li>
                <li>
                  Allow us reasonable time to investigate and address the issue
                  before public disclosure.
                </li>
                <li>
                  We will acknowledge receipt within 48 hours and provide
                  updates throughout the remediation process.
                </li>
              </ol>
            </div>
            <p className="text-gray-600">
              For critical vulnerabilities, we offer a bug bounty reward at our
              discretion. Thank you for helping keep Nester safe!
            </p>
          </section>
        </div>
      </main>
    </>
  );
}
