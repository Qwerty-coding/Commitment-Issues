/**
 * Language detection for the lightweight syntax highlighter.
 *
 * Kept in its own module (not alongside the Highlight component) so the
 * component file only exports React components, satisfying
 * react-refresh/only-export-components.
 */

export const EXT_TO_LANG = {
  js: 'javascript',
  jsx: 'javascript',
  mjs: 'javascript',
  cjs: 'javascript',
  ts: 'typescript',
  tsx: 'typescript',
  py: 'python',
  go: 'go',
  java: 'java',
  cs: 'csharp',
  rs: 'rust',
  rb: 'ruby',
  c: 'c',
  h: 'c',
  cpp: 'cpp',
  cc: 'cpp',
  cxx: 'cpp',
  hpp: 'cpp',
  sql: 'sql',
}

/** languageForFile maps a filename/path to a highlight language, or null. */
export function languageForFile(filename) {
  if (!filename) return null
  const ext = String(filename).split('.').pop()?.toLowerCase()
  return EXT_TO_LANG[ext] || null
}