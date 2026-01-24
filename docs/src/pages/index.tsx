import React from 'react';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import CodeBlock from '@theme/CodeBlock';
import styles from './index.module.css';

function Header() {
    return (
        <header className={styles.header}>
            <div className={styles.logo}>blob</div>
            <nav className={styles.navRow}>
                <Link to="/docs" className={styles.navLink}>Docs</Link>
                <Link to="https://github.com/meigma/blob" className={styles.navLink}>GitHub</Link>
            </nav>
        </header>
    );
}

function Hero() {
    return (
        <section className={styles.heroSection}>
            <Header />
            <div className={styles.heroContent}>
                <h1 className={styles.tagline}>
                    Supply chain security for everything that isn't a container.
                </h1>
                <p className={styles.subhead}>
                    Signed file archives in OCI registries. Cryptographic provenance wherever they go.
                </p>
                <div className={styles.ctaRow}>
                    <Link className={styles.ctaPrimary} to="/docs/getting-started">
                        Get Started
                    </Link>
                    <Link className={styles.ctaSecondary} to="https://github.com/meigma/blob">
                        View on GitHub
                    </Link>
                </div>
                <div className={styles.installSection}>
                    <span className={styles.installLabel}>QUICK INSTALL</span>
                    <div className={styles.installCode}>
                        <code className={styles.installText}>curl -fsSL https://blob.meigma.dev/install.sh | bash</code>
                    </div>
                    <span className={styles.installMeta}>or install via brew, scoop, go install</span>
                </div>
                <div className={styles.demoSection}>
                    <img
                        src="/img/demo.gif"
                        alt="blob CLI demo showing push, inspect, and pull commands"
                        className={styles.demoGif}
                    />
                </div>
            </div>
        </section>
    );
}

function Problem() {
    return (
        <section className={styles.problemSection}>
            <h2 className={styles.problemHeading}>
                Container images are signed. Everything else isn't.
            </h2>
            <p className={styles.problemCopy}>
                Config files, ML models, certificates, build artifacts—they move through your systems with zero provenance. No signatures. No attestations. No verification that they haven't been tampered with.
            </p>
            <p className={styles.problemHighlight}>
                We solved this problem for containers years ago. Everything else is still the wild west.
            </p>
        </section>
    );
}

function WhatBlobDoes() {
    const codeExample = `# Push configs to your registry
blob push ghcr.io/myorg/configs:v1 ./production/

# Sign with your identity (keyless via Sigstore)
blob sign ghcr.io/myorg/configs:v1

# Verify and pull on the other side
blob verify ghcr.io/myorg/configs:v1 --policy policy.yaml
blob pull ghcr.io/myorg/configs:v1 ./`;

    return (
        <section className={styles.whatSection}>
            <h2 className={styles.whatHeading}>
                Blob brings container-grade security to file archives.
            </h2>
            <p className={styles.whatCopy}>
                Push any directory to an OCI registry. Sign it with Sigstore. Verify it with policies. Extract exactly what you need—without downloading the whole thing.
            </p>
            <div className={styles.codeBlock}>
                <CodeBlock language="bash">{codeExample}</CodeBlock>
            </div>
        </section>
    );
}

function Benefits() {
    const benefits = [
        {
            icon: 'shield-check',
            title: 'Know where it came from',
            description: 'Every file is individually hashed. Manifests are signed. Attestations travel with the artifact. Tamper with one byte and verification fails.',
        },
        {
            icon: 'download',
            title: 'Download only what you need',
            description: 'Extract a single file from a 10GB archive without downloading 10GB. HTTP range requests fetch exactly the bytes you need.',
        },
        {
            icon: 'cloud',
            title: 'Uses registries you already have',
            description: 'Works with GitHub Container Registry, ECR, GCR, Docker Hub—any OCI-compliant registry. No new infrastructure required.',
        },
    ];

    return (
        <section className={styles.benefitsSection}>
            <div className={styles.benefitsGrid}>
                {benefits.map((benefit) => (
                    <div key={benefit.title} className={styles.benefitCard}>
                        <div className={styles.benefitIcon}>
                            <Icon name={benefit.icon} />
                        </div>
                        <h3 className={styles.benefitTitle}>{benefit.title}</h3>
                        <p className={styles.benefitCopy}>{benefit.description}</p>
                    </div>
                ))}
            </div>
        </section>
    );
}

function HowItWorks() {
    return (
        <section className={styles.howItWorksSection}>
            <h2 className={styles.howHeading}>Two blobs. One insight.</h2>
            <div className={styles.architectureDiagram}>
                <div className={styles.manifestBox}>
                    <span className={styles.manifestLabel}>Signed Manifest</span>
                </div>
                <span className={styles.manifestNote}>Signed &amp; attested</span>
                <div className={styles.arrowsRow}>
                    <Icon name="arrow-down" className={styles.arrowIcon} />
                    <Icon name="arrow-down" className={styles.arrowIcon} />
                </div>
                <div className={styles.blobsRow}>
                    <div className={styles.blobCard}>
                        <span className={styles.blobTitle}>Index</span>
                        <span className={styles.blobSubtitle}>(tiny)</span>
                        <span className={styles.blobDesc}>Metadata, paths, hashes</span>
                    </div>
                    <div className={styles.blobCard}>
                        <span className={styles.blobTitle}>Data</span>
                        <span className={styles.blobSubtitle}>(content)</span>
                        <span className={styles.blobDesc}>Files sorted by path</span>
                    </div>
                </div>
            </div>
            <p className={styles.howCopy}>
                The index stores metadata—paths, sizes, hashes—in a compact format with instant lookups. The data blob stores file contents sorted by path, so entire directories can be fetched in a single request.
            </p>
            <p className={styles.howCopy2}>
                Signing the manifest cryptographically binds everything together. Modify any file, anywhere, and verification fails.
            </p>
        </section>
    );
}

function UseCases() {
    const useCases = [
        {
            icon: 'settings',
            title: 'Configuration',
            description: 'Distribute configs with proof of origin',
        },
        {
            icon: 'cpu',
            title: 'ML Models',
            description: 'Large files with integrity verification',
        },
        {
            icon: 'file-badge',
            title: 'Certificates',
            description: 'Security-critical files with tamper detection',
        },
        {
            icon: 'package',
            title: 'Build Artifacts',
            description: 'CI outputs with SLSA provenance',
        },
    ];

    return (
        <section className={styles.useCasesSection}>
            <span className={styles.useCasesLabel}>BUILT FOR</span>
            <div className={styles.useCasesGrid}>
                {useCases.map((useCase) => (
                    <div key={useCase.title} className={styles.useCaseCard}>
                        <div className={styles.useCaseIcon}>
                            <Icon name={useCase.icon} />
                        </div>
                        <h3 className={styles.useCaseTitle}>{useCase.title}</h3>
                        <p className={styles.useCaseDesc}>{useCase.description}</p>
                    </div>
                ))}
            </div>
        </section>
    );
}

function FinalCTA() {
    const codeExample = `# Install
curl -fsSL https://blob.meigma.dev/install.sh | bash

# Try it
blob open ghcr.io/meigma/examples:hello-world`;

    return (
        <section className={styles.ctaSection}>
            <h2 className={styles.ctaHeading}>Get started in 30 seconds</h2>
            <div className={styles.ctaCodeBlock}>
                <CodeBlock language="bash">{codeExample}</CodeBlock>
            </div>
            <div className={styles.ctaButtonRow}>
                <Link className={styles.ctaButtonPrimary} to="/docs/getting-started">
                    Read the Docs
                </Link>
                <Link className={styles.ctaButtonSecondary} to="https://github.com/meigma/blob">
                    <Icon name="star" className={styles.ctaButtonIcon} />
                    Star on GitHub
                </Link>
            </div>
        </section>
    );
}

function Footer() {
    return (
        <footer className={styles.footer}>
            <div className={styles.footerContent}>
                <div className={styles.footerBrand}>
                    <span className={styles.footerLogo}>blob</span>
                    <span className={styles.footerTagline}>Supply chain security for everything.</span>
                </div>
                <nav className={styles.footerLinks}>
                    <Link to="/docs" className={styles.footerLink}>Docs</Link>
                    <Link to="https://github.com/meigma/blob" className={styles.footerLink}>GitHub</Link>
                    <Link to="https://github.com/meigma/blob/blob/master/LICENSE" className={styles.footerLink}>Apache 2.0</Link>
                </nav>
            </div>
            <div className={styles.footerDivider} />
            <div className={styles.footerBottom}>
                <span className={styles.footerCopyright}>© 2025 Meigma</span>
                <span className={styles.footerMeigma}>Built by Meigma</span>
            </div>
        </footer>
    );
}

// Simple icon component using Lucide icon names
function Icon({ name, className = '' }: { name: string; className?: string }) {
    const icons: Record<string, JSX.Element> = {
        'shield-check': (
            <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
                <path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"/>
                <path d="m9 12 2 2 4-4"/>
            </svg>
        ),
        'download': (
            <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
                <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
                <polyline points="7 10 12 15 17 10"/>
                <line x1="12" x2="12" y1="15" y2="3"/>
            </svg>
        ),
        'cloud': (
            <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
                <path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/>
            </svg>
        ),
        'settings': (
            <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
                <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/>
                <circle cx="12" cy="12" r="3"/>
            </svg>
        ),
        'cpu': (
            <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
                <rect x="4" y="4" width="16" height="16" rx="2"/>
                <rect x="9" y="9" width="6" height="6"/>
                <path d="M15 2v2"/>
                <path d="M15 20v2"/>
                <path d="M2 15h2"/>
                <path d="M2 9h2"/>
                <path d="M20 15h2"/>
                <path d="M20 9h2"/>
                <path d="M9 2v2"/>
                <path d="M9 20v2"/>
            </svg>
        ),
        'file-badge': (
            <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
                <path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/>
                <circle cx="12" cy="10" r="3"/>
                <path d="M14 18h-4v-2l1-1 1-1 1 1 1 1Z"/>
            </svg>
        ),
        'package': (
            <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
                <path d="m7.5 4.27 9 5.15"/>
                <path d="M21 8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z"/>
                <path d="m3.3 7 8.7 5 8.7-5"/>
                <path d="M12 22V12"/>
            </svg>
        ),
        'arrow-down': (
            <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
                <path d="M12 5v14"/>
                <path d="m19 12-7 7-7-7"/>
            </svg>
        ),
        'star': (
            <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}>
                <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/>
            </svg>
        ),
    };

    return icons[name] || null;
}

export default function Home(): JSX.Element {
    const { siteConfig } = useDocusaurusContext();
    return (
        <Layout
            title={siteConfig.tagline}
            description="Supply chain security for file archives. Signed artifacts in OCI registries with cryptographic provenance."
        >
            <main className={styles.landingPage}>
                <Hero />
                <Problem />
                <WhatBlobDoes />
                <Benefits />
                <HowItWorks />
                <UseCases />
                <FinalCTA />
                <Footer />
            </main>
        </Layout>
    );
}
