# Sprout Documentation Website

Modern documentation website for Sprout built with Docusaurus 3, Bun, and Lucide icons.

## Design Features

### Modern Landing Page
- **Sprout Branding** - Emerald green color scheme reflecting growth and nature
- **Lucide Icons** - Clean, modern iconography throughout
- **Git Worktree Visualization** - Animated SVG background showing worktree structure
- **Glassmorphism UI** - Modern frosted glass effects and gradients
- **AI-Focused Design** - Contemporary design suitable for developer tools

### Visual Elements
- Animated git worktree graph in hero background
- Gradient orbs and grid patterns
- Terminal-style demo sections
- Feature cards with icon badges
- One-click install with copy button

## Quick Start

### From Project Root

```bash
# Generate docs and start dev server
make docs-dev

# Build production site
make docs-build

# Just generate docs (without starting server)
make docs-generate
```

### From This Directory

```bash
# Install dependencies (if needed)
bun install

# Generate documentation and start dev server
bun run dev

# Just start dev server (without generating)
bun start

# Build for production
bun run build

# Serve production build locally
bun serve
```

## Documentation Generation

Documentation is semi-automated:

### Auto-Generated
- **CLI Commands** (`docs/cli/commands.md`) - Generated from Go CLI code
- **Configuration Reference** (`docs/configuration/reference.md`) - Generated from Config struct

### Manual
- All other documentation in `docs/`
- Landing page (`src/pages/index.tsx`)
- Components (`src/components/`)

### Generation Scripts

Located in `scripts/`:
- `generate-cli-docs.go` - Extracts CLI command information
- `generate-config-docs.go` - Extracts configuration options
- `generate-docs.sh` - Orchestrates generation

## Customization

### Brand Colors

Edit `src/css/custom.css` to modify the emerald green theme:

```css
:root {
  --ifm-color-primary: #10b981;  /* Emerald 500 */
  --ifm-color-primary-dark: #059669;  /* Emerald 600 */
  /* ... */
}
```

### Landing Page

Edit `src/pages/index.tsx` for homepage customization:
- Hero section with animated background
- Feature cards
- Demo sections
- CTA section

### Icons

Using Lucide React icons. Import from `lucide-react`:

```tsx
import { Sprout, Terminal, GitBranch } from 'lucide-react';
```

## Project Structure

```
apps/web/
├── docs/                     # Documentation content
│   ├── intro.md
│   ├── installation.md
│   ├── quick-start.md
│   ├── concepts/
│   ├── cli/                  # Auto-generated
│   ├── configuration/
│   ├── integrations/
│   └── troubleshooting/
├── src/
│   ├── components/           # React components
│   │   └── HomepageFeatures/
│   ├── css/
│   │   └── custom.css        # Brand colors & global styles
│   └── pages/
│       ├── index.tsx         # Landing page
│       └── index.module.css  # Landing page styles
├── scripts/                  # Generation scripts
│   ├── generate-cli-docs.go
│   ├── generate-config-docs.go
│   └── generate-docs.sh
├── static/                   # Static assets
│   └── img/
├── docusaurus.config.ts      # Docusaurus configuration
├── sidebars.ts               # Sidebar structure
└── package.json
```

## Development Tips

### Live Reload
Changes to most files trigger hot reload. For generation script changes, re-run `bun run docs:generate`.

### Dark Mode
Design is optimized for both light and dark modes with adjusted colors and opacity.

### Animations
- Pulsing git nodes
- Fade-in worktree branches
- Floating gradient orbs
- Smooth hover effects

## Dependencies

- **Docusaurus 3.9.2** - Static site generator
- **React 19** - UI framework
- **Lucide React** - Icon library
- **Bun** - Fast JavaScript runtime

## Building for Production

```bash
# From root
make docs-build

# From this directory
bun run build
```

Output is in `build/` directory.

## Deployment

The site can be deployed to:
- GitHub Pages
- Vercel
- Netlify
- Cloudflare Pages
- Any static hosting service

See [Docusaurus Deployment Guide](https://docusaurus.io/docs/deployment) for details.

## Learn More

- [Docusaurus Documentation](https://docusaurus.io/)
- [Lucide Icons](https://lucide.dev/)
- [MDX Documentation](https://mdxjs.com/)
- [Bun Documentation](https://bun.sh/docs)

