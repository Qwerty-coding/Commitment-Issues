import { useMemo } from 'react'
import './highlight.css'

/**
 * Lightweight, dependency-free syntax highlighter.
 *
 * The conflict/diff panes previously rendered code as plain monospace text,
 * which makes comparing versions hard. Rather than pull in Prism/highlight.js
 * (extra weight + a network install), this tokenizes the common languages
 * with a small regex scanner covering the constructs that matter for reading
 * merges: comments, strings, numbers, keywords and identifiers.
 *
 * It is intentionally best-effort, not a full parser: correctness of display
 * never depends on it, and unknown languages fall back to plain text.
 */

const KEYWORDS = {
  javascript:
    'const let var function return if else for while do switch case break continue class extends new this super import export from default async await try catch finally throw typeof instanceof in of null undefined true false yield static get set',
  typescript:
    'const let var function return if else for while do switch case break continue class extends implements interface type enum new this super import export from default async await try catch finally throw typeof instanceof in of null undefined true false public private protected readonly abstract as satisfies declare namespace',
  python:
    'def class return if elif else for while break continue import from as try except finally raise with lambda pass global nonlocal yield assert del in is not and or None True False async await match case',
  go: 'package import func return if else for range break continue switch case default type struct interface map chan go defer select var const nil true false',
  java: 'public private protected class interface extends implements static final void new return if else for while do switch case break continue try catch finally throw throws import package this super null true false abstract synchronized volatile transient instanceof enum record',
  csharp:
    'using namespace class interface struct public private protected internal static readonly const void new return if else for foreach while do switch case break continue try catch finally throw this base null true false async await var record enum abstract virtual override sealed',
  rust: 'fn let mut const struct enum impl trait pub use mod return if else for while loop match break continue self Self where async await move ref dyn as in unsafe true false',
  ruby: 'def class module end return if elsif else unless for while until break next yield begin rescue ensure raise require include extend attr_reader attr_writer attr_accessor nil true false self',
  c: 'int char long short float double void struct union enum typedef static extern const unsigned signed return if else for while do switch case break continue sizeof goto',
  cpp: 'int char long short float double void struct class union enum typedef static extern const unsigned signed return if else for while do switch case break continue sizeof new delete namespace using public private protected virtual template typename nullptr true false auto',
  sql: 'SELECT FROM WHERE INSERT INTO VALUES UPDATE SET DELETE CREATE TABLE ALTER DROP INDEX JOIN LEFT RIGHT INNER OUTER ON GROUP BY ORDER HAVING LIMIT AS AND OR NOT NULL PRIMARY KEY FOREIGN REFERENCES',
}

// Build one combined regex per language. Order matters: comments and strings
// are matched before identifiers so keywords inside them are not highlighted.
function buildScanner(language) {
  const kw = KEYWORDS[language]
  const keywordPattern = kw ? kw.trim().split(/\s+/).join('|') : null
  const parts = [
    '(?<comment>\\/\\/[^\\n]*|#[^\\n]*|\\/\\*[\\s\\S]*?\\*\\/)',
    '(?<string>`(?:[^`\\\\]|\\\\.)*`|"(?:[^"\\\\]|\\\\.)*"|\'(?:[^\'\\\\]|\\\\.)*\')',
    '(?<number>\\b\\d[\\d_]*(?:\\.\\d+)?(?:[eE][+-]?\\d+)?\\b)',
  ]
  if (keywordPattern) {
    parts.push(`(?<keyword>\\b(?:${keywordPattern})\\b)`)
  }
  return new RegExp(parts.join('|'), 'g')
}

const scannerCache = new Map()

function getScanner(language) {
  if (!language) return null
  if (!scannerCache.has(language)) {
    scannerCache.set(language, buildScanner(language))
  }
  return scannerCache.get(language)
}

const CLASS_BY_GROUP = {
  comment: 'tok-comment',
  string: 'tok-string',
  number: 'tok-number',
  keyword: 'tok-keyword',
}

/**
 * Highlight renders `code` as spans with token classes. When no language is
 * known (or the scanner finds nothing) it renders the raw text unchanged, so
 * it is always safe as a drop-in replacement for <code>{code}</code>.
 */
export default function Highlight({ code, language, className }) {
  const text = code == null ? '' : String(code)

  const tokens = useMemo(() => {
    const scanner = getScanner(language)
    if (!scanner) return null
    scanner.lastIndex = 0
    const out = []
    let last = 0
    let match
    while ((match = scanner.exec(text)) !== null) {
      if (match.index > last) {
        out.push({ text: text.slice(last, match.index), cls: null })
      }
      const groups = match.groups || {}
      const cls = CLASS_BY_GROUP[Object.keys(groups).find((k) => groups[k] !== undefined)]
      out.push({ text: match[0], cls: cls || null })
      last = match.index + match[0].length
      // Guard against zero-length matches looping forever.
      if (match[0].length === 0) scanner.lastIndex++
    }
    if (last < text.length) {
      out.push({ text: text.slice(last), cls: null })
    }
    return out
  }, [text, language])

  if (!tokens) {
    return <code className={className}>{text}</code>
  }

  return (
    <code className={className}>
      {tokens.map((t, i) =>
        t.cls ? (
          <span key={i} className={t.cls}>
            {t.text}
          </span>
        ) : (
          <span key={i}>{t.text}</span>
        ),
      )}
    </code>
  )
}