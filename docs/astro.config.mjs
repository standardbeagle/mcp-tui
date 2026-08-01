import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
  site: 'https://dev.standardbeagle.com',
  base: '/mcp-tui',
  integrations: [
    starlight({
      title: 'MCP-TUI',
      description:
        'Fast terminal UI and CLI for testing, debugging, and automating Model Context Protocol servers. STDIO, SSE, and HTTP transports with structured output for CI.',
      logo: { src: './src/assets/logo.svg', replacesTitle: false },
      social: [
        { icon: 'github', label: 'GitHub', href: 'https://github.com/standardbeagle/mcp-tui' },
      ],
      head: [
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
          label: 'Guides',
          items: [
            { label: 'TUI Mode', slug: 'guides/tui' },
            { label: 'CLI Mode', slug: 'guides/cli' },
            { label: 'Transports', slug: 'guides/transports' },
            { label: 'OAuth', slug: 'guides/oauth' },
            { label: 'Client Features', slug: 'guides/client-features' },
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
