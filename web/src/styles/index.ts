// The app and Storybook consume the same global cascade and self-hosted fonts.
// Component sheets are reached through app.css, including native styled links.
import '@fontsource-variable/instrument-sans';
import '@fontsource/ibm-plex-mono/400.css';
import '@fontsource/ibm-plex-mono/500.css';
import './tokens.css';
import './app.css';
