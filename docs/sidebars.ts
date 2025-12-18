import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

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
      label: 'Core Concepts',
      items: [
        'terminology',
        'languages',
        'services',
        'scaling',
        'disks',
        'firewall',
      ],
    },
    'working-with-miren-cloud',
    {
      type: 'category',
      label: 'CLI Reference',
      link: {
        type: 'doc',
        id: 'cli-reference',
      },
      items: [
        'cli/app',
        'cli/logs',
        'cli/sandbox',
        'cli/disk',
        'cli/entity',
      ],
    },
    {
      type: 'category',
      label: 'Resources',
      items: [
        'changelog',
        'support',
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
