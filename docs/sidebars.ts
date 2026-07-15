import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';
import commandSidebar from './command-sidebar.json';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

/**
 * Creating a sidebar enables you to:
 - create an ordered group of docs
 - render a sidebar for each doc of that group
 - provide next/previous navigation

 The sidebars can be generated from the filesystem, or explicitly defined here.

 Create as many sidebars as you want.
 */
const sidebars: SidebarsConfig = {
  tutorialSidebar: [
    'intro',
    'getting-started',
    {
      type: 'category',
      label: 'Deploy',
      collapsed: false,
      items: [
        'deployment',
        'app-configuration',
        'languages',
        'services',
        'ci-deploy',
        'pr-environments',
      ],
    },
    {
      type: 'category',
      label: 'Language Guides',
      // Nearly 40 guides; unfurled they dominate the sidebar. The theme still
      // auto-expands this category when the reader is on a guide page.
      collapsed: true,
      link: {
        type: 'doc',
        id: 'guides/index',
      },
      items: [
        'guides/python',
        'guides/javascript',
        'guides/go',
        'guides/ruby',
        'guides/rust',
        'guides/elixir',
        'guides/gleam',
        'guides/crystal',
        'guides/zig',
        'guides/nim',
        'guides/c',
        'guides/cpp',
        'guides/objc',
        'guides/deno',
        'guides/dotnet',
        'guides/fsharp',
        'guides/java',
        'guides/kotlin',
        'guides/scala',
        'guides/clojure',
        'guides/erlang',
        'guides/php',
        'guides/perl',
        'guides/raku',
        'guides/ocaml',
        'guides/haskell',
        'guides/swift',
        'guides/dart',
        'guides/jruby',
        'guides/truffleruby',
        'guides/julia',
        'guides/r',
        'guides/lua',
        'guides/klong',
        'guides/commonlisp',
        'guides/cobol',
        'guides/bash',
        'guides/static',
      ],
    },
    {
      type: 'category',
      label: 'Recipes',
      collapsed: false,
      items: [
        'recipes/hermes-agent',
      ],
    },
    {
      type: 'category',
      label: 'Data & Storage',
      collapsed: false,
      items: [
        'disks',
        'addons',
      ],
    },
    {
      type: 'category',
      label: 'Networking & Security',
      collapsed: false,
      items: [
        'traffic-routing',
        'tls',
        'firewall',
        'waf',
        'route-protect',
        'workload-identity',
      ],
    },
    {
      type: 'category',
      label: 'Run & Scale',
      collapsed: false,
      items: [
        'scaling',
        'admin-interface',
        'observability',
        'logs',
      ],
    },
    {
      type: 'category',
      label: 'Miren Cloud',
      collapsed: false,
      items: [
        {
          type: 'doc',
          id: 'miren-cloud/overview',
          label: 'Overview',
        },
        'miren-cloud/subdomains',
        'miren-cloud/connectivity',
        {
          type: 'doc',
          id: 'miren-cloud/cloud-updates',
          label: 'Updates',
        },
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      collapsed: false,
      items: [
        'system-requirements',
        'app-toml',
        'server-config',
        {
          type: 'category',
          label: 'CLI',
          collapsed: true,
          link: {
            type: 'doc',
            id: 'commands',
          },
          items: commandSidebar as any[],
        },
      ],
    },
    {
      type: 'category',
      label: 'Resources',
      collapsed: false,
      items: [
        'miren-club',
        'agent-skills',
        'troubleshooting',
        'terminology',
        'labs',
        'changelog',
        'conduct',
        {
          type: 'link',
          label: 'How Miren Compares',
          href: 'https://miren.dev/compare',
        },
      ],
    },
  ],
};

export default sidebars;
