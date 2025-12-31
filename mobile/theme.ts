export type Theme = {
  background: string;
  card: string;
  border: string;
  textPrimary: string;
  textSecondary: string;
  accent: string;
};

export const lightTheme: Theme = {
  background: '#ffffff',
  card: '#f6f7fb',
  border: '#d8deea',
  textPrimary: '#0e1a2b',
  textSecondary: '#4a607a',
  accent: '#000000', // black accent for light mode
};

export const darkTheme: Theme = {
  background: '#05060a',
  card: '#0f1320',
  border: '#1f2a3c',
  textPrimary: '#e8ecf5',
  textSecondary: '#a5b3c9',
  accent: '#7c3aed', // purple accent for dark mode
};
