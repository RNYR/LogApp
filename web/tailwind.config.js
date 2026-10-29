/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        // Sklad brand palette (carried over from the legacy app and extended
        // into a usable scale).
        brand: {
          50: '#eaf7fd',
          100: '#cfeefb',
          200: '#a4dff6',
          300: '#6ccbee',
          400: '#33b3e0',
          500: '#0a99d1',
          600: '#0a7ba8',
          700: '#0b6287',
          800: '#0d506e',
          900: '#032e3f',
        },
        // Semantic design-system tokens (Phase 1).
        canvas: '#101820',
        panel: { DEFAULT: 'rgb(var(--panel) / <alpha-value>)', subtle: 'rgb(var(--panel-subtle) / <alpha-value>)' },
        // `inverse` and `onColor` are guaranteed-contrast foregrounds for fills
        // whose background is not a semantic token: arbitrary API status
        // colours select between them by WCAG relative luminance.
        content: { DEFAULT: 'rgb(var(--content) / <alpha-value>)', muted: 'rgb(var(--content-muted) / <alpha-value>)', inverse: '#ffffff', onColor: '#000000' },
        stroke: { DEFAULT: 'rgb(var(--stroke) / <alpha-value>)', control: '#657c8c' },
        action: {
          DEFAULT: '#78be5a',
          hover: '#91ce77',
          selected: 'rgb(var(--action-selected) / <alpha-value>)',
        },
        success: { fg: 'rgb(var(--success-fg) / <alpha-value>)', bg: 'rgb(var(--success-bg) / <alpha-value>)' },
        warning: { fg: 'rgb(var(--warning-fg) / <alpha-value>)', bg: 'rgb(var(--warning-bg) / <alpha-value>)' },
        danger: { fg: 'rgb(var(--danger-fg) / <alpha-value>)', bg: 'rgb(var(--danger-bg) / <alpha-value>)', fill: '#f5aaa7' },
        info: { fg: 'rgb(var(--info-fg) / <alpha-value>)', bg: 'rgb(var(--info-bg) / <alpha-value>)' },
      },
      fontFamily: {
        sans: [
          'Work Sans',
          'ui-sans-serif',
          'system-ui',
          '-apple-system',
          'Segoe UI',
          'Roboto',
          'Helvetica',
          'Arial',
          'sans-serif',
        ],
      },
      boxShadow: {
        // Restrained overlay elevation for dialogs/dropdowns (Phase 1).
        overlay: '0 24px 80px rgba(0, 0, 0, 0.45)',
      },
      keyframes: {
        'fade-in': {
          from: { opacity: '0', transform: 'translateY(4px)' },
          to: { opacity: '1', transform: 'translateY(0)' },
        },
      },
      animation: {
        'fade-in': 'fade-in 150ms ease-out',
      },
    },
  },
  plugins: [],
}
