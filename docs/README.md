# Miren Documentation

This directory contains the Miren documentation site built with [Docusaurus](https://docusaurus.io/).

## Development Setup

### For Nix Users (Recommended)

If you have Nix installed, the development environment is automatically configured with bun:

```bash
# Enter the Nix development shell (from repo root)
nix develop

# Navigate to docs directory
cd docs

# Install dependencies
bun install

# Start development server
bun start
```

The development server will be available at http://localhost:3000.

### For Non-Nix Users

If you don't use Nix, you'll need to install bun manually:

**Requirements:**
- Bun 1.0 or later

**Installation:**

1. Install bun from [bun.sh](https://bun.sh):
   ```bash
   # Linux/macOS
   curl -fsSL https://bun.sh/install | bash

   # Windows
   powershell -c "irm bun.sh/install.ps1 | iex"
   ```

2. Install dependencies:
   ```bash
   cd docs
   bun install
   ```

3. Start development server:
   ```bash
   bun start
   ```

## Available Commands

All commands should be run from the `docs/` directory:

### `bun start`

Starts the development server with hot-reloading at http://localhost:3000.

### `bun run build`

Builds the static site for production to the `build/` directory. The build is optimized and minified.

### `bun run serve`

Serves the production build locally for testing. Run `bun run build` first.

### `bun run clear`

Clears the Docusaurus cache. Useful when encountering build issues.

### `bun run typecheck`

Runs TypeScript type checking without emitting files.

## Project Structure

```
docs/
├── docs/                  # Documentation markdown files
│   ├── intro.md          # Home page
│   ├── getting-started/  # Getting started guides
│   └── cli/              # CLI reference documentation
├── src/
│   ├── css/              # Custom CSS and theming
│   ├── components/       # Custom React components
│   └── pages/            # Custom pages (non-docs)
├── static/               # Static assets (images, files)
│   └── img/              # Images and logos
├── docusaurus.config.ts  # Docusaurus configuration
├── sidebars.ts           # Sidebar navigation structure
├── package.json          # Node.js dependencies and scripts
└── tsconfig.json         # TypeScript configuration
```

## Documentation Guidelines

### Writing Content

- Use clear, concise language
- Include code examples where appropriate
- Follow the existing structure and tone
- Test all code examples before committing

### Adding New Pages

1. Create a new `.md` or `.mdx` file in the appropriate directory under `docs/`
2. Add frontmatter at the top:
   ```md
   ---
   sidebar_position: 1
   ---
   ```
3. The sidebar will auto-generate based on the file structure

### Styling

- Brand colors and fonts are defined in `src/css/custom.css`
- The site uses Miren's brand colors (Topaz blue #0059FF)
- Typography: Hanken Grotesk for body text, DM Mono for code

## Deployment

The documentation is automatically deployed to GitHub Pages when changes are pushed to the `main` branch.

### GitHub Pages Setup

The site is configured to deploy to https://miren.md via GitHub Pages:

1. GitHub Actions workflow (`.github/workflows/docs.yml`) builds the site
2. The build is deployed to the `gh-pages` branch
3. Custom domain `miren.md` is configured via `static/CNAME`

### Manual Deployment

To deploy manually (rarely needed):

```bash
bun run build
# The build output in build/ can be deployed to any static host
```

## Troubleshooting

### Port Already in Use

If port 3000 is already in use, start the server on a different port:

```bash
bun start --port 3001
```

### Cache Issues

Clear the cache and rebuild:

```bash
bun run clear
bun run build
```

### TypeScript Errors

Check for type errors:

```bash
bun run typecheck
```

### Bun Version Issues

Ensure you're using Bun 1.0 or later:

```bash
bun --version  # Should show 1.x.x or higher
```

## Contributing

1. Make changes to the documentation
2. Test locally with `bun start`
3. Build to verify: `bun run build`
4. Commit and push your changes
5. The docs will automatically deploy on merge to `main`

## Resources

- [Docusaurus Documentation](https://docusaurus.io/docs)
- [Markdown Features](https://docusaurus.io/docs/markdown-features)
- [Miren Brand Guidelines](../tmp/brand/BRAND.md)
