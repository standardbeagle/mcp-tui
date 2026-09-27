import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
  site: 'https://dev.standardbeagle.com',
  base: '/mcp-tui',
  integrations: [
    starlight({
      title: 'MCP-TUI',
      description:
        'Test, debug and automate Model Context Protocol servers from the terminal: run MCP tools from schema-built forms, diagnose connection and handshake errors, check spec compliance, and script it for CI.',
      logo: { src: './src/assets/logo.svg', replacesTitle: false },
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/standardbeagle/mcp-tui' },
      ],
      head: [
        {
          tag: 'meta',
          attrs: {
            name: 'googlebot',
            content: 'noindex, follow',
          },
        },
        {
          tag: 'script',
          attrs: {
            type: 'module',
            src: 'https://static.cloudflareinsights.com/beacon.min.js',
            'data-cf-beacon': '{"token": "e77d64f1f6f24ed9b18d06d0320e7d1a"}',
          },
        },
        {
          tag: 'meta',
          attrs: {
            name: 'keywords',
            content:
              'MCP, Model Context Protocol, MCP client, MCP CLI, MCP TUI, MCP debugger, MCP testing, AI agent tools, Claude MCP, JSON-RPC, STDIO, SSE, streamable HTTP',
          },
        },
        {
          tag: 'meta',
          attrs: { property: 'og:type', content: 'website' },
        },
        {
          tag: 'meta',
          attrs: {
            property: 'og:image',
            content: 'https://dev.standardbeagle.com/mcp-tui/og.png',
          },
        },
        {
          tag: 'meta',
          attrs: { name: 'twitter:card', content: 'summary_large_image' },
        },
        {
          tag: 'meta',
          attrs: {
            property: 'og:image:alt',
            content: 'mcp-tui running a tool from a form built from its input schema, with the result beside it',
          },
        },
        {
          tag: 'script',
          attrs: { type: 'application/ld+json' },
          content: JSON.stringify({
            '@context': 'https://schema.org',
            '@type': 'SoftwareApplication',
            name: 'MCP-TUI',
            applicationCategory: 'DeveloperApplication',
            operatingSystem: 'Linux, macOS, Windows',
            description:
              'Terminal UI and CLI for testing, debugging and automating Model Context Protocol servers.',
            url: 'https://dev.standardbeagle.com/mcp-tui/',
            downloadUrl: 'https://github.com/standardbeagle/mcp-tui/releases',
            license: 'https://opensource.org/licenses/MIT',
            offers: { '@type': 'Offer', price: '0', priceCurrency: 'USD' },
            author: { '@type': 'Organization', name: 'Standard Beagle', url: 'https://standardbeagle.com' },
          }),
        },
      ],
      sidebar: [
        {
          label: 'Get Started',
          items: [
            { label: 'Overview', slug: '' },
            { label: 'Install', slug: 'install' },
            { label: 'Quick Start', slug: 'quick-start' },
          ],
        },
        {
          label: 'How-to',
          items: [
            { label: 'All how-to guides', slug: 'how-to' },
            { label: 'Test a tool in seconds', slug: 'how-to/test-an-mcp-tool' },
            { label: 'Manual checks to CI scripts', slug: 'how-to/ci-scripts' },
            { label: 'Fix connection errors', slug: 'how-to/fix-connection-errors' },
            { label: 'Validate tool arguments', slug: 'how-to/validate-tool-arguments' },
            { label: 'Inspect MCP traffic', slug: 'how-to/inspect-traffic' },
            { label: 'Check spec compliance', slug: 'how-to/check-spec-compliance' },
            { label: 'Progress, logs, tasks', slug: 'how-to/async-features' },
            { label: 'Test OAuth', slug: 'how-to/test-oauth' },
            { label: 'Tool metadata', slug: 'how-to/tool-metadata' },
            { label: 'Resources and prompts', slug: 'how-to/resources-and-prompts' },
            { label: 'Elicitation and sampling', slug: 'how-to/elicitation-and-sampling' },
            { label: 'Protocol versions', slug: 'how-to/protocol-versions' },
            { label: 'Videos', slug: 'videos' },
          ],
        },
        {
          label: 'Guides',
          items: [
            { label: 'TUI Mode', slug: 'guides/tui' },
            { label: 'CLI Mode', slug: 'guides/cli' },
            { label: 'Transports', slug: 'guides/transports' },
            { label: 'Protocol 2026-07-28', slug: 'guides/protocol-2026-07-28' },
            { label: 'OAuth', slug: 'guides/oauth' },
            { label: 'Client Features', slug: 'guides/client-features' },
            { label: 'Tasks', slug: 'guides/tasks' },
            { label: 'Configuration', slug: 'guides/configuration' },
            { label: 'Automation & CI', slug: 'guides/automation' },
            { label: 'Conformance Testing', slug: 'guides/testing' },
            { label: 'Debugging', slug: 'guides/debugging' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { label: 'CLI Reference', slug: 'reference/cli' },
            { label: 'Error Messages', slug: 'reference/errors' },
            { label: 'Keyboard Shortcuts', slug: 'reference/keyboard' },
            { label: 'Architecture', slug: 'reference/architecture' },
          ],
        },
      ],
      customCss: ['./src/styles/custom.css'],
      editLink: {
        baseUrl: 'https://github.com/standardbeagle/mcp-tui/edit/main/docs/',
      },
    }),
  ],
});
