# React + TypeScript + Vite

This template provides a minimal setup to get React working in Vite with HMR and some ESLint rules.

## Agent console

`/dashboard/agent` is the intelligence surface the `hyperagent` daemon
computes, plus a global chat drawer available from every page (toggle via the
CHAT button in the top nav, or press Esc to close it).

The page shows, top to bottom:

- **Status strip** — daemon connectivity, execution mode (propose/autonomous),
  and the batch/chat model providers.
- **Liquidity regime board** — one row per tracked market: price, funding
  (with a spark), OI delta, CVD, basis, and liquidation proximity.
- **Agent theses** — the latest reasoned verdict per asset (action, size,
  entry/stop/target, and the written thesis), ranked by confidence.
- **Pending proposals** — propose-mode candidates awaiting Approve/Reject;
  rejection reasons from the risk gates are shown verbatim.
- **Decision log** — the day's journal (candidates, fills, opens/closes,
  alerts, errors), with prev/next-day navigation.

All of it, plus the chat drawer, talks to the daemon's unified core API
instead of Hyperliquid directly. **The daemon must be running** for any of
this to populate — everything else in the dashboard (prices, portfolio) works
without it. Start it from the `tui/` directory:

```sh
./hyperagent -headless -testnet
```

It binds `http://127.0.0.1:8787` after a short warmup. With the daemon down,
the agent console and chat drawer render an explicit "offline" state (skeleton
rows / disabled input) rather than spinning or crashing.

To point the dashboard at a daemon running somewhere other than
`127.0.0.1:8787`, set `VITE_CORE_URL` (e.g. in `.env.local`):

```
VITE_CORE_URL=http://127.0.0.1:8787
```

Currently, two official plugins are available:

- [@vitejs/plugin-react](https://github.com/vitejs/vite-plugin-react/blob/main/packages/plugin-react) uses [Oxc](https://oxc.rs)
- [@vitejs/plugin-react-swc](https://github.com/vitejs/vite-plugin-react/blob/main/packages/plugin-react-swc) uses [SWC](https://swc.rs/)

## React Compiler

The React Compiler is not enabled on this template because of its impact on dev & build performances. To add it, see [this documentation](https://react.dev/learn/react-compiler/installation).

## Expanding the ESLint configuration

If you are developing a production application, we recommend updating the configuration to enable type-aware lint rules:

```js
export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      // Other configs...

      // Remove tseslint.configs.recommended and replace with this
      tseslint.configs.recommendedTypeChecked,
      // Alternatively, use this for stricter rules
      tseslint.configs.strictTypeChecked,
      // Optionally, add this for stylistic rules
      tseslint.configs.stylisticTypeChecked,

      // Other configs...
    ],
    languageOptions: {
      parserOptions: {
        project: ['./tsconfig.node.json', './tsconfig.app.json'],
        tsconfigRootDir: import.meta.dirname,
      },
      // other options...
    },
  },
])
```

You can also install [eslint-plugin-react-x](https://github.com/Rel1cx/eslint-react/tree/main/packages/plugins/eslint-plugin-react-x) and [eslint-plugin-react-dom](https://github.com/Rel1cx/eslint-react/tree/main/packages/plugins/eslint-plugin-react-dom) for React-specific lint rules:

```js
// eslint.config.js
import reactX from 'eslint-plugin-react-x'
import reactDom from 'eslint-plugin-react-dom'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      // Other configs...
      // Enable lint rules for React
      reactX.configs['recommended-typescript'],
      // Enable lint rules for React DOM
      reactDom.configs.recommended,
    ],
    languageOptions: {
      parserOptions: {
        project: ['./tsconfig.node.json', './tsconfig.app.json'],
        tsconfigRootDir: import.meta.dirname,
      },
      // other options...
    },
  },
])
```
