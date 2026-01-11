/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    "./internal/web/templates/**/*.html",
  ],
  theme: {
    extend: {
      colors: {
        // Primary brand color
        primary: {
          DEFAULT: '#0d6efd',
          hover: '#0b5ed7',
          light: '#cfe2ff',
        },
        // Severity colors
        severity: {
          critical: '#dc3545',
          'critical-bg': '#f8d7da',
          'critical-border': '#f5c2c7',
          warning: '#ffc107',
          'warning-bg': '#fff3cd',
          'warning-border': '#ffecb5',
          info: '#0dcaf0',
          'info-bg': '#cff4fc',
          'info-border': '#b6effb',
          success: '#198754',
          'success-bg': '#d1e7dd',
          'success-border': '#badbcc',
        },
        // Cache ratio colors
        cache: {
          excellent: '#198754',
          good: '#20c997',
          warning: '#ffc107',
          critical: '#dc3545',
        },
        // Neutral/UI colors
        muted: '#6c757d',
        border: '#dee2e6',
        bg: {
          DEFAULT: '#f8f9fa',
          dark: '#e9ecef',
          card: '#ffffff',
        },
      },
      fontFamily: {
        mono: ['SFMono-Regular', 'Consolas', 'Liberation Mono', 'Menlo', 'monospace'],
      },
      maxWidth: {
        'container': '1400px',
      },
      boxShadow: {
        'card': '0 1px 3px rgba(0, 0, 0, 0.1)',
        'card-hover': '0 4px 12px rgba(0, 0, 0, 0.1)',
      },
    },
  },
  plugins: [],
}
