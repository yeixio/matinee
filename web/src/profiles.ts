// Profiles: who's watching on a shared sign-in, and kids mode. The server
// filters what kids profiles see; these helpers decide what the web player
// shows around that. The phone and TV apps mirror them in src/profiles.ts.
import type { ProfileChoice, User } from './api'

// Avatar colours, by the names the server stores. They mark people, so they
// stay the same in every theme.
export const avatarColors: Record<string, string> = {
  coral: '#ff6b5b',
  amber: '#f5a524',
  lime: '#8bc34a',
  teal: '#26a69a',
  sky: '#42a5f5',
  violet: '#9575cd',
  pink: '#ec6fa9',
  slate: '#78909c',
}

export const avatarColor = (avatar?: string) => avatarColors[avatar ?? ''] ?? avatarColors.slate

// Kids levels, from the youngest up, with the US ratings each adds.
export const kidsLevels = [
  { level: 1, name: 'Little kids', ratings: 'G, TV-Y, TV-G' },
  { level: 2, name: 'Kids', ratings: 'PG, TV-Y7, TV-PG and younger' },
  { level: 3, name: 'Teens', ratings: 'PG-13, TV-14 and younger' },
]

export const kidsLevelName = (level?: number) => kidsLevels.find((l) => l.level === level)?.name ?? ''

export const isKids = (u?: Pick<User, 'kidsLevel'>) => !!u?.kidsLevel

// The account a user is, or belongs to as a profile.
export const accountOf = (u: Pick<User, 'id' | 'accountId'>) => u.accountId ?? u.id

const askedKey = 'matinee-profile-asked'

// shouldAsk says whether to show "Who's watching?": the account has
// profiles and this browser hasn't picked one since signing in. The server
// remembers the pick, so it isn't asked on every visit.
export function shouldAsk(me: User, profiles: ProfileChoice[] | undefined, asked: string | null): boolean {
  return !!profiles && profiles.length > 1 && asked !== String(accountOf(me))
}

export function askedFor(): string | null {
  try {
    return localStorage.getItem(askedKey)
  } catch {
    return null
  }
}

export function rememberAsked(me: User) {
  try {
    localStorage.setItem(askedKey, String(accountOf(me)))
  } catch {
    // private window: ask again next time
  }
}

export function forgetAsked() {
  try {
    localStorage.removeItem(askedKey)
  } catch {
    // nothing stored
  }
}
