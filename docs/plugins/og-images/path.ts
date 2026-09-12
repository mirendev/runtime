// Where a doc's card lives, relative to the site root. Shared by the plugin
// that writes the PNGs and the theme component that points og:image at them,
// so the two can never disagree about the naming.
//
// The permalink is used as-is under /img/og so the card tree mirrors the site:
// /disks -> /img/og/disks.png, /next/disks -> /img/og/next/disks.png. The root
// page and any permalink that ends in a slash become index.png.
export function ogImagePath(permalink: string): string {
  const dir = permalink.endsWith('/') ? `${permalink}index` : permalink;
  return `/img/og${dir}.png`;
}
