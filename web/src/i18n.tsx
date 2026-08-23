import { Fragment, createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { langCookie, readCookie, writeCookie } from './cookies'

// Every JSON file under locales/ is a language, and nothing here names one.
// Adding a language is therefore the whole job of dropping its file into that
// directory; the build compiles the directory into the bundle. The interface is
// embedded in the binary, so a new file needs `make ui` before it is served.
const modules = import.meta.glob<Record<string, string>>('./locales/*.json', {
  eager: true,
  import: 'default',
})

/** The language a browser gets when it has asked for none this build carries. */
export const defaultLanguage = 'en'

/** labelKey names a language in its own language, so the picker reads right. */
const labelKey = '$label'

const catalogs: Record<string, Record<string, string>> = {}
for (const [path, messages] of Object.entries(modules)) {
  const file = path.slice(path.lastIndexOf('/') + 1)
  catalogs[file.slice(0, -'.json'.length)] = messages
}

/** A language this build carries, ready for the picker. */
export interface Language {
  code: string
  label: string
}

export const languages: Language[] = Object.entries(catalogs)
  .map(([code, messages]) => ({ code, label: messages[labelKey] || code }))
  .sort((a, b) => a.label.localeCompare(b.label))

export function isSupported(code: string): boolean {
  return Object.hasOwn(catalogs, code)
}

// A partial translation falls through to English rather than showing the key,
// so a fork can ship a file that covers only what it has translated so far.
const fallback = catalogs[defaultLanguage] ?? {}

/** Looks one message up and fills in its {placeholders}. */
function translate(code: string, key: string, vars?: Record<string, string | number>): string {
  const message = catalogs[code]?.[key] ?? fallback[key] ?? key
  if (!vars) return message
  return message.replace(/\{(\w+)\}/g, (whole, name: string) =>
    Object.hasOwn(vars, name) ? String(vars[name]) : whole,
  )
}

export type Translate = (key: string, vars?: Record<string, string | number>) => string

interface I18n {
  lang: string
  t: Translate
  setLang: (code: string) => void
}

const I18nContext = createContext<I18n | null>(null)

/**
 * Resolves the stored language.
 *
 * An absent cookie and a cookie naming a language this build does not carry are
 * the same case: English, written back so the stored value and the shown
 * language never disagree.
 */
function initialLanguage(): string {
  const stored = readCookie(langCookie)
  if (isSupported(stored)) return stored
  writeCookie(langCookie, defaultLanguage)
  return defaultLanguage
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState(initialLanguage)

  const setLang = useCallback((code: string) => {
    const next = isSupported(code) ? code : defaultLanguage
    setLangState(next)
    writeCookie(langCookie, next)
  }, [])

  // The document has to name its own language for a screen reader to pronounce
  // it and for the browser to pick the right hyphenation.
  useEffect(() => {
    document.documentElement.lang = lang
  }, [lang])

  const value = useMemo<I18n>(
    () => ({ lang, setLang, t: (key, vars) => translate(lang, key, vars) }),
    [lang, setLang],
  )

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

export function useI18n(): I18n {
  const value = useContext(I18nContext)
  if (!value) {
    throw new Error('useI18n was called outside I18nProvider')
  }
  return value
}

/**
 * Renders a message whose {placeholders} stand for elements rather than text.
 *
 * The whole sentence stays one catalog entry, so a translator can move the
 * placeholders wherever their language needs them instead of translating
 * fragments whose order this file would decide.
 */
export function Formatted({ text, parts }: { text: string; parts: Record<string, ReactNode> }) {
  const nodes: ReactNode[] = []
  const pattern = /\{(\w+)\}/g
  let cursor = 0
  let match: RegExpExecArray | null

  while ((match = pattern.exec(text)) !== null) {
    const name = match[1] ?? ''
    if (!Object.hasOwn(parts, name)) continue
    nodes.push(text.slice(cursor, match.index))
    nodes.push(<Fragment key={match.index}>{parts[name]}</Fragment>)
    cursor = match.index + match[0].length
  }
  nodes.push(text.slice(cursor))

  return <>{nodes}</>
}
