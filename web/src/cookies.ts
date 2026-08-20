// Browser storage goes through cookies. localStorage is not used anywhere in
// this project.

const oneYear = 60 * 60 * 24 * 365

export function readCookie(name: string): string {
  const prefix = `${encodeURIComponent(name)}=`
  for (const part of document.cookie.split(';')) {
    const entry = part.trim()
    if (entry.startsWith(prefix)) {
      return decodeURIComponent(entry.slice(prefix.length))
    }
  }
  return ''
}

export function writeCookie(name: string, value: string): void {
  const secure = location.protocol === 'https:' ? '; Secure' : ''
  document.cookie =
    `${encodeURIComponent(name)}=${encodeURIComponent(value)}` +
    `; path=/; max-age=${oneYear}; SameSite=Strict${secure}`
}

export function clearCookie(name: string): void {
  document.cookie = `${encodeURIComponent(name)}=; path=/; max-age=0; SameSite=Strict`
}

export const apiKeyCookie = 'm365bridge_api_key'
export const modelCookie = 'm365bridge_model'
