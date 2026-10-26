import { describe, expect, it } from 'vitest'
import type { ProfileChoice, User } from './api'
import { accountOf, avatarColor, kidsLevelName, shouldAsk } from './profiles'

const me: User = { id: 1, username: 'mike', isAdmin: true }
const sam: User = { id: 7, username: 'Sam', isAdmin: false, accountId: 1, kidsLevel: 2 }
const choices: ProfileChoice[] = [
  { id: 1, name: 'mike', hasPin: false, account: true },
  { id: 7, name: 'Sam', hasPin: false, kidsLevel: 2 },
]

describe('profiles', () => {
  it('asks who is watching once per account, and only with profiles', () => {
    expect(shouldAsk(me, choices, null)).toBe(true)
    expect(shouldAsk(me, choices, '1')).toBe(false)
    expect(shouldAsk(sam, choices, '1')).toBe(false)
    expect(shouldAsk(me, choices, '2')).toBe(true)
    expect(shouldAsk(me, choices.slice(0, 1), null)).toBe(false)
    expect(shouldAsk(me, undefined, null)).toBe(false)
  })

  it('finds the account of a profile', () => {
    expect(accountOf(me)).toBe(1)
    expect(accountOf(sam)).toBe(1)
  })

  it('names levels and colours', () => {
    expect(kidsLevelName(2)).toBe('Kids')
    expect(kidsLevelName(0)).toBe('')
    expect(avatarColor('teal')).toBe('#26a69a')
    expect(avatarColor('nope')).toBe(avatarColor('slate'))
  })
})
