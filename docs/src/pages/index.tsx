import React from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';
import CodeBlock from '@theme/CodeBlock';
import styles from './index.module.css';

function Hero() {
    return (
        <header className={clsx('hero hero-ledger', styles.heroBanner)}>
            <div className="container">
                <div className="row hero-grid">
                    <div className="col col--6 hero-copy">
                        <p className="hero-kicker">OCI-native file artifacts</p>
                        <h1 className="hero__title">
                            Every file, <span className="text-gradient">provably authentic.</span>
                        </h1>
                        <p className="hero__subtitle hero-lede">
                            Sign and attest file archives in OCI registries. Carry cryptographic provenance wherever they go.
                        </p>
                        <div className={clsx(styles.buttons, 'cta-row')}>
                            <Link className="button button--primary button--lg" to="/docs/getting-started">
                                Get Started
                            </Link>
                            <Link className="button button--secondary button--lg" to="https://github.com/meigma/blob">
                                View on GitHub
                            </Link>
                        </div>
                        <div className="hero-stat">
                            <span className="stat-value">99.99%</span>
                            <span className="stat-label">less bandwidth on partial reads</span>
                        </div>
                    </div>
                    <div className="col col--6 hero-visual">
                        <div className="ledger-card">
                            <div className="ledger-card-header">
                                <span className="ledger-badge">Integrity Ledger</span>
                                <span className="ledger-tag">Signed chain of custody</span>
                            </div>
                            <div className="ledger-strip">
                                <div className="ledger-scan"></div>
                                <div className="ledger-node">
                                    <div className="ledger-node-title">Signed</div>
                                    <div className="ledger-node-desc">Sigstore signature</div>
                                    <code>sig-2f9a</code>
                                </div>
                                <div className="ledger-node">
                                    <div className="ledger-node-title">Manifest</div>
                                    <div className="ledger-node-desc">Manifest digest</div>
                                    <code>sha256:9d2f...c8e1</code>
                                </div>
                                <div className="ledger-node">
                                    <div className="ledger-node-title">Index</div>
                                    <div className="ledger-node-desc">Index blob digests</div>
                                    <code>idx:3c91</code>
                                </div>
                                <div className="ledger-node">
                                    <div className="ledger-node-title">Per-file</div>
                                    <div className="ledger-node-desc">SHA256 on read</div>
                                    <code>file:7b1e</code>
                                </div>
                            </div>
                            <div className="ledger-footer">
                                <span>Per-file verification, end-to-end.</span>
                                <span className="ledger-stamp">Verified</span>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </header>
    );
}

function Problem() {
    return (
        <section className="section-contrast">
            <div className="container">
                <div className="row row--align-center">
                    <div className="col col--7">
                        <p className="section-kicker">The gap</p>
                        <h2>
                            You sign your container images. <span className="soft-contrast">What about everything else?</span>
                        </h2>
                        <p className="section-lede">
                            Config files. ML models. Deployment artifacts. Certificates. They move between systems with
                            <strong> no provenance</strong>, <strong>no integrity</strong>, and full downloads every time.
                        </p>
                    </div>
                    <div className="col col--5">
                        <div className="comparison-card">
                            <div className="comparison-row">
                                <span className="comparison-label">Unsigned file</span>
                                <span className="comparison-badge danger">No provenance</span>
                            </div>
                            <div className="comparison-row">
                                <span className="comparison-label">Blob file</span>
                                <span className="comparison-badge success">Verified</span>
                            </div>
                            <div className="comparison-hash">
                                <span>sha256</span>
                                <code>1f8c...b91a</code>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </section>
    );
}

function Proofs() {
    const proofs = [
        {
            title: 'Prove origin',
            label: 'Sigstore + SLSA',
            desc: 'Attach signatures and attestations to file archives in OCI registries—then verify every file on read.',
        },
        {
            title: 'Verify on read',
            label: 'Per-file SHA256',
            desc: 'Tamper with a single byte and verification fails instantly.',
        },
        {
            title: 'Only fetch what you use',
            label: 'Range reads',
            desc: 'Browse and stream without downloading a 1GB archive.',
        },
    ];

    return (
        <section className="section-deep">
            <div className="container">
                <div className="section-head">
                    <p className="section-kicker">Trust, end-to-end</p>
                    <h2>Make provenance visible, then prove it.</h2>
                    <p className="section-lede">
                        Blob turns file archives into verifiable, browsable artifacts that behave like container images.
                    </p>
                </div>
                <div className="proof-grid stagger">
                    {proofs.map((proof) => (
                        <div key={proof.title} className="proof-card">
                            <div className="proof-label">{proof.label}</div>
                            <h3>{proof.title}</h3>
                            <p>{proof.desc}</p>
                        </div>
                    ))}
                </div>
                <div className="chain-wrapper">
                    <div className="chain-line">
                        <div className="chain-node">
                            <span>Signed</span>
                            <small>Sigstore signature</small>
                        </div>
                        <div className="chain-node">
                            <span>Manifest</span>
                            <small>Digest anchor</small>
                        </div>
                        <div className="chain-node">
                            <span>Index Blob</span>
                            <small>File map</small>
                        </div>
                        <div className="chain-node">
                            <span>Per-file</span>
                            <small>SHA256 on read</small>
                        </div>
                    </div>
                    <div className="chain-callout">Every file inherits the signature above it.</div>
                </div>
            </div>
        </section>
    );
}

function ProvenanceFlow() {
    const steps = [
        { title: 'Build', label: 'GitHub Actions' },
        { title: 'Sign', label: 'Sigstore' },
        { title: 'Attest', label: 'SLSA provenance' },
        { title: 'Push', label: 'OCI registry' },
        { title: 'Pull', label: 'Consumer system' },
        { title: 'Verify + Extract', label: 'Policy checks on read' },
    ];

    return (
        <section className="section-deep">
            <div className="container">
                <div className="section-head">
                    <p className="section-kicker">Flow</p>
                    <h2>Provenance Flow</h2>
                    <p className="section-lede">Same guarantees as container images, for any file.</p>
                </div>
                <div className="flow-grid stagger">
                    {steps.map((step, index) => (
                        <div key={step.title} className="flow-step">
                            <div className="flow-number">{index + 1}</div>
                            <div>
                                <h3>{step.title}</h3>
                                <p>{step.label}</p>
                            </div>
                        </div>
                    ))}
                </div>
                <div className="flow-example">Example: GitHub Actions → GHCR → production host</div>
            </div>
        </section>
    );
}

function Performance() {
    return (
        <section className="section-contrast">
            <div className="container">
                <div className="performance-header">
                    <p className="section-kicker">Performance</p>
                    <h2>Built for speed at file granularity.</h2>
                    <p className="section-lede">
                        Why download 1GB to read a 64KB config? Blob uses HTTP Range Requests to fetch exactly what you
                        need, when you need it.
                    </p>
                </div>
                <Tabs
                    className="bench-tabs"
                    defaultValue="overview"
                    values={[
                        { label: 'Overview', value: 'overview' },
                        { label: 'Detailed Benchmarks', value: 'details' },
                        { label: 'vs eStargz', value: 'estargz' },
                    ]}
                >
                    <TabItem value="overview">
                        <div className="bench-panel overview-panel">
                            <div className="bandwidth-card overview-hero">
                                <div className="bandwidth-row">
                                    <div className="bandwidth-label">Traditional archive</div>
                                    <div className="bandwidth-bar">
                                        <span className="bandwidth-fill full"></span>
                                    </div>
                                    <span className="bandwidth-meta">1.0 GB</span>
                                </div>
                                <div className="bandwidth-row">
                                    <div className="bandwidth-label">Blob range fetch</div>
                                    <div className="bandwidth-bar">
                                        <span className="bandwidth-fill tiny"></span>
                                    </div>
                                    <span className="bandwidth-meta accent">~65 KB</span>
                                </div>
                            </div>
                            <div className="overview-stats">
                                <div className="overview-stat-card">
                                    <div className="overview-stat-value">26 ns</div>
                                    <div className="overview-stat-label">lookup</div>
                                </div>
                                <div className="overview-stat-card">
                                    <div className="overview-stat-value">43x</div>
                                    <div className="overview-stat-label">faster</div>
                                </div>
                                <div className="overview-stat-card">
                                    <div className="overview-stat-value">99.99%</div>
                                    <div className="overview-stat-label">saved</div>
                                </div>
                            </div>
                        </div>
                    </TabItem>
                    <TabItem value="details">
                        <div className="bench-panel">
                            <div className="bench-detail">
                                <div className="bench-hero">
                                    <p className="bench-hero-kicker">Bandwidth efficiency</p>
                                    <div className="bench-hero-grid">
                                        <div className="bench-hero-row">
                                            <span className="bench-hero-label">Archive size</span>
                                            <div className="bench-hero-track">
                                                <span className="bench-hero-fill bench-hero-fill-full"></span>
                                            </div>
                                            <span className="bench-hero-value">1 GB</span>
                                        </div>
                                        <div className="bench-hero-row">
                                            <span className="bench-hero-label">Data fetched</span>
                                            <div className="bench-hero-track">
                                                <span className="bench-hero-fill bench-hero-fill-tiny"></span>
                                            </div>
                                            <span className="bench-hero-value">64 KB</span>
                                        </div>
                                    </div>
                                    <p className="bench-hero-note">&lt;- 99.99% never downloaded</p>
                                </div>

                                <div className="bench-card-grid">
                                    <div className="bench-stat-card">
                                        <p className="bench-stat-title">Index is tiny</p>
                                        <div className="bench-stat-value">108 B</div>
                                        <p className="bench-stat-desc">per file - 10K files = 1 MB index</p>
                                    </div>
                                    <div className="bench-stat-card">
                                        <p className="bench-stat-title">Instant lookups</p>
                                        <div className="bench-stat-value">26 ns</div>
                                        <p className="bench-stat-desc">constant time - any archive size</p>
                                    </div>
                                    <div className="bench-stat-card">
                                        <p className="bench-stat-title">Batch efficiency</p>
                                        <div className="bench-stat-value">32 files, 1 request</div>
                                        <p className="bench-stat-desc">directories fetch as a single contiguous read</p>
                                    </div>
                                </div>

                                <div className="bench-chart-grid">
                                    <div className="bench-chart">
                                        <div className="bench-chart-header">
                                            <h3>Network Reality</h3>
                                            <p>Read latency (64 KiB file, 5ms RTT) - cache effect</p>
                                        </div>
                                        <div className="bench-chart-row">
                                            <div className="bench-chart-label">
                                                <span>Cache Cold</span>
                                                <span className="bench-chart-value">6.27 ms</span>
                                            </div>
                                            <div className="bench-track">
                                                <div className="bench-bar bench-bar-warn" style={{ width: '100%' }}></div>
                                            </div>
                                        </div>
                                        <div className="bench-chart-row">
                                            <div className="bench-chart-label">
                                                <span>Cache Warm</span>
                                                <span className="bench-chart-value">0.14 ms</span>
                                            </div>
                                            <div className="bench-track">
                                                <div className="bench-bar" style={{ width: '2.3%' }}></div>
                                            </div>
                                        </div>
                                        <div className="bench-chart-foot">43x faster</div>
                                    </div>

                                    <div className="bench-chart bench-cache">
                                        <div className="bench-chart-header">
                                            <h3>Index Cache</h3>
                                            <p>Open once, instant forever</p>
                                        </div>
                                        <div className="bench-cache-body">
                                            <div className="bench-cache-row">
                                                <div className="bench-cache-label">
                                                    <span>First open</span>
                                                    <span className="bench-cache-value">6.9 ms</span>
                                                </div>
                                                <div className="bench-cache-track">
                                                    <span className="bench-cache-fill bench-cache-fill-full"></span>
                                                </div>
                                            </div>
                                            <div className="bench-cache-row">
                                                <div className="bench-cache-label">
                                                    <span>Repeat open</span>
                                                    <span className="bench-cache-value">0.39 us</span>
                                                </div>
                                                <div className="bench-cache-track">
                                                    <span className="bench-cache-fill bench-cache-fill-tiny"></span>
                                                </div>
                                            </div>
                                        </div>
                                        <div className="bench-cache-foot">17,700x faster</div>
                                    </div>

                                    <div className="bench-chart bench-table-card">
                                        <div className="bench-chart-header">
                                            <h3>Proven Scaling</h3>
                                            <p>Performance remains constant as archive grows</p>
                                        </div>
                                        <div className="bench-scale-flow">
                                            <div className="bench-scale-axis">
                                                <span className="bench-scale-axis-label bench-scale-axis-label-top">files</span>
                                                <span className="bench-scale-axis-label bench-scale-axis-label-bottom">index</span>
                                                <div className="bench-scale-node">
                                                    <span className="bench-scale-count">100</span>
                                                    <span className="bench-scale-dot"></span>
                                                    <span className="bench-scale-index">10 KB</span>
                                                </div>
                                                <div className="bench-scale-node">
                                                    <span className="bench-scale-count">1K</span>
                                                    <span className="bench-scale-dot"></span>
                                                    <span className="bench-scale-index">108 KB</span>
                                                </div>
                                                <div className="bench-scale-node">
                                                    <span className="bench-scale-count">10K</span>
                                                    <span className="bench-scale-dot"></span>
                                                    <span className="bench-scale-index">1 MB</span>
                                                </div>
                                                <div className="bench-scale-node">
                                                    <span className="bench-scale-count">100K</span>
                                                    <span className="bench-scale-dot"></span>
                                                    <span className="bench-scale-index">10 MB</span>
                                                </div>
                                            </div>
                                            <div className="bench-scale-lookup">
                                                <span className="bench-scale-lookup-track"></span>
                                                <span className="bench-scale-lookup-value">26 ns</span>
                                            </div>
                                            <p className="bench-scale-caption">(constant lookup time)</p>
                                        </div>
                                    </div>
                                </div>
                            </div>
                        </div>
                    </TabItem>
                    <TabItem value="estargz">
                        <div className="bench-panel">
                            <div className="estargz-card">
                                <div className="estargz-header">
                                    <h3>Blob Performance Impact</h3>
                                    <p>Relative speedup vs eStargz (remote HTTP)</p>
                                </div>
                                <div className="estargz-chart">
                                    <div className="estargz-row">
                                        <span>Random Access (Read)</span>
                                        <div className="estargz-track">
                                            <span className="estargz-baseline"></span>
                                            <span className="estargz-fill" style={{ width: '95.3%' }}>
                                                <span className="estargz-value">14.3x</span>
                                            </span>
                                        </div>
                                    </div>
                                    <div className="estargz-row">
                                        <span>Cold Start (Open)</span>
                                        <div className="estargz-track">
                                            <span className="estargz-baseline"></span>
                                            <span className="estargz-fill" style={{ width: '55.3%' }}>
                                                <span className="estargz-value">8.3x</span>
                                            </span>
                                        </div>
                                    </div>
                                    <div className="estargz-row">
                                        <span>Build &amp; Publish</span>
                                        <div className="estargz-track">
                                            <span className="estargz-baseline"></span>
                                            <span className="estargz-fill" style={{ width: '22%' }}>
                                                <span className="estargz-value">3.3x</span>
                                            </span>
                                        </div>
                                    </div>
                                    <div className="estargz-row">
                                        <span>Bulk Transfer</span>
                                        <div className="estargz-track">
                                            <span className="estargz-baseline"></span>
                                            <span className="estargz-fill" style={{ width: '20%' }}>
                                                <span className="estargz-value">3.0x</span>
                                            </span>
                                        </div>
                                    </div>
                                </div>
                                <div className="estargz-legend">
                                    <div>
                                        <span className="estargz-dot estargz-dot-blob"></span>
                                        Blob (speedup)
                                    </div>
                                    <div>
                                        <span className="estargz-dot estargz-dot-base"></span>
                                        eStargz (baseline 1x)
                                    </div>
                                </div>
                                <div className="estargz-footer">
                                    Hardware: AMD EPYC 9124 (16-core), 128GB RAM, NVMe (Latitude.sh m4-metal-medium)
                                </div>
                            </div>
                        </div>
                    </TabItem>
                </Tabs>
            </div>
        </section>
    );
}

function CodeExample() {
    return (
        <section className="section-deep">
            <div className="container">
                <div className="row">
                    <div className="col col--8 col--offset-2">
                        <div className="code-intro">
                            <p className="section-kicker">API</p>
                            <h2>Simple Go API</h2>
                            <div className="code-bullets">
                                <span>Push with signing in one call.</span>
                                <span>Pull with policy enforcement and lazy reads.</span>
                            </div>
                        </div>
                        <div className="code-frame">
                            <CodeBlock language="go">
                                {`// Push with signing
client.Push(ctx, "ghcr.io/org/configs:v1", archive)

// Pull with verification
c := client.New(
    client.WithPolicy(sigstore.NewPolicy(...)),
    client.WithPolicy(opa.NewPolicy(...)),
)

// Lazy load - only downloads what you read
archive, _ := c.Pull(ctx, "ghcr.io/org/configs:v1")
archive.CopyDir("./output", "configs/")`}
                            </CodeBlock>
                        </div>
                    </div>
                </div>
            </div>
        </section>
    );
}

export default function Home(): JSX.Element {
    const { siteConfig } = useDocusaurusContext();
    return (
        <Layout title={`Blob - ${siteConfig.tagline}`} description="The secure, lazy-loading archive format for OCI registries.">
            <main>
                <Hero />
                <Problem />
                <Proofs />
                <ProvenanceFlow />
                <Performance />
                <CodeExample />
            </main>
        </Layout>
    );
}
