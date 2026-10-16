import {readFileSync, writeFileSync} from 'fs';
import {join, relative} from 'path';
import type {LoadContext, Plugin} from '@docusaurus/types';
import type {LoadedContent} from '@docusaurus/plugin-content-docs';
import llmsPlugin from 'docusaurus-plugin-llms';

// Wraps docusaurus-plugin-llms and regroups the llms.txt it writes into
// sections that mirror the docs sidebar. The plugin can only emit headings
// when each section is its own directory, and most of our pages sit flat in
// one, so left alone it lists every page under a single "Table of Contents"
// in filesystem order. Taking the sections from the sidebar means there is
// one taxonomy to keep current: a page added to the sidebar shows up here in
// the same place, and one missing from it lands in Optional rather than
// disappearing.
//
// This has to wrap the plugin rather than sit beside it. Docusaurus runs every
// plugin's postBuild concurrently, so a sibling rewriting llms.txt would race
// the plugin writing it.

type Options = {
  // Passed through to docusaurus-plugin-llms untouched.
  llms: Record<string, unknown>;
  // Doc ids that stand in for the sidebar category containing them. The
  // category becomes that one link, so a reader gets an index page instead of
  // every entry under it.
  pointers: string[];
  // Heading for docs that sit at the top of the sidebar outside any category.
  leadSection: string;
};

type SidebarItem = {
  type: string;
  id?: string;
  label?: string;
  items?: SidebarItem[];
  link?: {type: string; id?: string};
};

type Section = {title: string; ids: string[]};

// Doc ids in sidebar order, including a category's own linked doc.
function docIds(items: SidebarItem[]): string[] {
  return items.flatMap((item) => {
    if (item.type === 'doc' || item.type === 'ref') return [item.id!];
    if (item.type === 'category') {
      const link = item.link?.type === 'doc' ? [item.link.id!] : [];
      return [...link, ...docIds(item.items ?? [])];
    }
    return [];
  });
}

// The ids to list for a run of sidebar items, collapsing any category that
// holds a pointer down to that pointer. Every id beneath it, listed or not,
// goes into `covered` so it isn't swept into Optional.
function listed(
  items: SidebarItem[],
  pointers: Set<string>,
  covered: Set<string>,
  collapsed: Set<string>,
): string[] {
  return items.flatMap((item) => {
    if (item.type !== 'category') {
      const ids = docIds([item]);
      ids.forEach((id) => covered.add(id));
      return ids;
    }
    // Only the category the pointer sits in directly, as its link or one of
    // its own items, collapses: commands is the CLI category's link, and
    // Reference around it keeps its other pages.
    const own = [
      ...(item.link?.type === 'doc' ? [item.link.id!] : []),
      ...(item.items ?? []).flatMap((i) => (i.type === 'doc' ? [i.id!] : [])),
    ];
    const pointer = own.find((id) => pointers.has(id));
    if (pointer) {
      docIds([item]).forEach((id) => covered.add(id));
      collapsed.add(pointer);
      return [pointer];
    }
    return [
      ...(item.link?.type === 'doc' ? [item.link.id!] : []),
      ...listed(item.items ?? [], pointers, covered, collapsed),
    ];
  });
}

function sections(
  sidebar: SidebarItem[],
  options: Options,
  covered: Set<string>,
): Section[] {
  const pointers = new Set(options.pointers);
  const collapsed = new Set<string>();
  const out: Section[] = [];
  for (const item of sidebar) {
    if (item.type === 'category') {
      out.push({
        title: item.label!,
        ids: listed([item], pointers, covered, collapsed),
      });
    } else {
      // Loose docs join the section before them, or open the list.
      if (out.length === 0) out.push({title: options.leadSection, ids: []});
      out[out.length - 1].ids.push(
        ...listed([item], pointers, covered, collapsed),
      );
    }
  }
  // A pointer that matches nothing lets its category expand back into every
  // entry, the very list this plugin exists to collapse, so it fails instead.
  const unmatched = options.pointers.filter((id) => !collapsed.has(id));
  if (unmatched.length > 0) {
    throw new Error(
      `[llms-sections] pointers not found in any sidebar category: ${unmatched.join(', ')}`,
    );
  }
  return out;
}

export default function llmsSectionsPlugin(
  context: LoadContext,
  options: Options,
): Plugin<unknown> {
  const inner = llmsPlugin(context, options.llms as any) as Plugin<unknown>;

  return {
    ...inner,
    name: 'llms-sections',

    async postBuild(props) {
      await inner.postBuild!(props);

      const docsPlugin = props.plugins.find(
        (p) => p.name === 'docusaurus-plugin-content-docs',
      );
      const versions = (docsPlugin!.content as LoadedContent).loadedVersions;
      // The plugin reads only the docs served at the site root: the released
      // snapshot when there is one, otherwise this branch's docs.
      const root = versions.find((v) => v.path === context.baseUrl);
      if (!root) {
        throw new Error('[llms-sections] no docs version at the site root');
      }

      const file = join(props.outDir, 'llms.txt');
      const text = readFileSync(file, 'utf8');
      const marker = '## Table of Contents\n\n';
      const at = text.indexOf(marker);
      if (at < 0) {
        throw new Error(
          `[llms-sections] no "${marker.trim()}" in llms.txt; has docusaurus-plugin-llms changed its output?`,
        );
      }
      const header = text.slice(0, at);
      const entries = text
        .slice(at + marker.length)
        .split('\n')
        .filter((line) => line.startsWith('- ['));

      // The plugin names each page's Markdown copy after its source path,
      // which the docs plugin also knows, so that is what ties a sidebar id to
      // its entry. Slugs don't enter into it.
      const siteUrl = context.siteConfig.url + context.baseUrl;
      const byPath = new Map<string, string>();
      for (const line of entries) {
        const url = line.match(/\]\(([^)]+)\)/)?.[1] ?? '';
        byPath.set(url.slice(siteUrl.length).replace(/\.mdx?$/, ''), line);
      }
      const byId = new Map<string, string>();
      for (const doc of root.docs) {
        const source = doc.source.replace(/^@site\//, '');
        const path = relative(
          relative(context.siteDir, root.contentPath),
          source,
        ).replace(/\.mdx?$/, '');
        const line = byPath.get(path);
        if (line) byId.set(doc.id, line);
      }

      const covered = new Set<string>();
      const sidebar = Object.values(root.sidebars)[0] as SidebarItem[];
      const blocks = sections(sidebar, options, covered).map((section) => {
        const lines = section.ids.map((id) => {
          const line = byId.get(id);
          if (!line) {
            throw new Error(
              `[llms-sections] sidebar doc "${id}" has no llms.txt entry`,
            );
          }
          return line;
        });
        return `## ${section.title}\n\n${lines.join('\n')}`;
      });

      const optional = root.docs
        .filter((doc) => !covered.has(doc.id) && byId.has(doc.id))
        .map((doc) => byId.get(doc.id)!);
      if (optional.length > 0) {
        blocks.push(`## Optional\n\n${optional.join('\n')}`);
      }

      writeFileSync(file, `${header}${blocks.join('\n\n')}\n`);
    },
  };
}
