// Types for matinee-player.js, for editors and TypeScript. Keep this file
// next to matinee-player.js in your player's folder.

/** A labelled line on a title's page, such as Manufacturer: Namco. */
export interface Fact {
  label: string
  value: string
}

/** The item being played. */
export interface Item {
  id: number
  libraryId: number
  /** One of the plugin's item kinds, such as "game". */
  kind: string
  title: string
  year?: number
  facts?: Fact[]
  /** What the plugin's server part kept with the item when scanning (Item.Data in the Go SDK). */
  pluginData: any
}

/** A stored value's key and size. */
export interface StoredKey {
  key: string
  size: number
  /** When it was last written, as an RFC 3339 time. */
  updatedAt: string
}

/** What a player can keep, per person and per plugin. */
export interface Storage {
  /** The keys starting with prefix, and how much the person's storage for this plugin uses of its limit, in bytes. */
  list(prefix?: string): Promise<{ keys: StoredKey[]; used: number; limit: number }>
  /** A stored value, or null if there's none. */
  get(key: string): Promise<ArrayBuffer | null>
  /**
   * Keeps a value of up to 32 MB. Keys are letters, digits, ".", "_", "-"
   * and "/", up to 200 characters. An ArrayBuffer is handed over, not
   * copied, so it can't be used afterwards.
   */
  put(key: string, value: ArrayBuffer | string): Promise<null>
  delete(key: string): Promise<null>
}

/** The connection to Matinee's web player. */
export interface Matinee {
  item: Item
  /** The language of the person's Matinee, such as "en" or "pt-BR". */
  language: string
  /** The item's file. */
  file(): Promise<ArrayBuffer>
  /**
   * Another file in a folder of the item's library, by its path in that
   * folder, such as "neogeo.zip". Only files the library kind is asked
   * about (its extensions) are handed over.
   */
  libraryFile(path: string): Promise<ArrayBuffer>
  storage: Storage
  /** Saves how far the person got; finished marks the item played. Plugins listening for playback events hear of it. */
  progress(positionMs: number, finished?: boolean): Promise<null>
  /** Closes the player and goes back to the title's page. */
  exit(): Promise<null>
}

/** Tells the web player the page is ready, and resolves once it says what to play. */
export function connect(): Promise<Matinee>
