// The pretend library the docs' screenshots show: original titles, people
// and music, made up for Matinee. demo/library.mjs turns it into files and
// demo/fake-tmdb.mjs answers the server's metadata lookups from it.

// TMDB's genre ids, which the server maps to its own names.
const G = { action: 28, adventure: 12, animation: 16, comedy: 35, crime: 80, drama: 18, family: 10751, fantasy: 14, history: 36, mystery: 9648, romance: 10749, scifi: 878, thriller: 53, western: 37 }
const TV = { kids: 10762, scififantasy: 10765, comedy: 35, drama: 18, mystery: 9648, animation: 16 }

export const people = [
  { id: 1, name: 'Ada Lindqvist', born: '1988-02-14', place: 'Bergen, Norway', bio: 'Ada Lindqvist is a Norwegian actress known for quiet, watchful roles. She trained at the Oslo National Academy of the Arts and spent a decade on stage before her first film.' },
  { id: 2, name: 'Tomas Reyes', born: '1979-07-02', place: 'Valparaíso, Chile', bio: 'Tomas Reyes started out as a cinema usher and still knows every seat in the house. He has played fishermen, priests and one very tired detective.' },
  { id: 3, name: 'June Okafor', born: '1995-11-23', place: 'Lagos, Nigeria', bio: 'June Okafor moved from music videos to features and is known for comedies with a sad streak.' },
  { id: 4, name: 'Felix Marchetti', born: '1966-04-09', place: 'Trieste, Italy', bio: 'A character actor with more than eighty credits, usually the man behind the counter.' },
  { id: 5, name: 'Noor Haddad', born: '2009-05-30', place: 'Amman, Jordan', bio: 'Noor Haddad made her debut at ten and has since voiced half a dozen animated heroines.' },
  { id: 6, name: 'Pieter de Vries', born: '1972-01-17', place: 'Utrecht, Netherlands', bio: 'Pieter de Vries splits his year between the stage in Amsterdam and small roles in big films.' },
  { id: 7, name: 'Lena Sato', born: '1990-09-12', place: 'Sapporo, Japan', bio: 'Lena Sato is an actress and radio host whose voice is better known than her face.' },
  { id: 8, name: 'Marcus Bell', born: '1983-03-03', place: 'Cardiff, Wales', bio: 'Marcus Bell plays leading men who would rather be somewhere else.' },
  { id: 9, name: 'Ines Duarte', born: '1992-12-01', place: 'Porto, Portugal', bio: 'Ines Duarte trained as a dancer and moved into film with a run of road movies.' },
  { id: 10, name: 'Kofi Mensah', born: '1987-08-19', place: 'Kumasi, Ghana', bio: 'Kofi Mensah is a comic actor and writer, and the voice of Bolt in Tiny Robots.' },
  { id: 11, name: 'Clara Weiss', born: '1975-06-25', place: 'Graz, Austria', bio: 'Clara Weiss is best known for science fiction, where she has played three ship captains and one ship.' },
  { id: 12, name: 'Ravi Menon', born: '1981-10-08', place: 'Kochi, India', bio: 'Ravi Menon came to acting from architecture and is drawn to films about places.' },
  { id: 20, name: 'Iris Holloway', born: '1970-03-21', place: 'Hobart, Australia', known: 'Directing', bio: 'Iris Holloway writes and directs slow, luminous films about islands and the people who stay on them.' },
  { id: 21, name: 'Sam Achebe', born: '1984-01-11', place: 'Enugu, Nigeria', known: 'Sound', bio: 'Sam Achebe is a composer whose scores lean on old pianos and tape loops.' },
  { id: 22, name: 'Hana Kowalski', born: '1979-05-05', place: 'Gdańsk, Poland', known: 'Directing', bio: 'Hana Kowalski directs animation and the occasional live-action fable.' },
  { id: 23, name: 'Oscar Lund', born: '1968-11-30', place: 'Malmö, Sweden', known: 'Directing', bio: 'Oscar Lund makes genre films on small budgets and big skies.' },
]

const cast = (...list) => list.map(([id, character], order) => ({ id, character, order }))

// Movies. scene picks the artwork (art.mjs); hue tints it for titles that
// share a scene.
export const movies = [
  {
    id: 101, title: 'The Long Matinee', year: 2024, date: '2024-03-08', minutes: 124, genres: [G.drama, G.mystery, G.fantasy], cert: 'PG-13', vote: 8.1,
    tagline: 'Tomorrow is already showing.',
    overview: 'A lighthouse keeper on a remote island finds a reel of film that shows tomorrow. Each night she watches, and each day she tries to change what she saw, until the island begins to change around her.',
    cast: cast([1, 'Mara Quill'], [2, 'The Projectionist'], [3, 'Elsie'], [4, 'Harbormaster'], [5, 'Young Mara'], [6, 'Ferryman'], [7, 'Radio Voice']),
    crew: [[20, 'Director'], [20, 'Screenplay'], [21, 'Original Music Composer']],
    studios: ['Lantern Pictures'], countries: ['Norway', 'United Kingdom'], budget: 18000000, revenue: 64200000,
    collection: { id: 900, name: 'The Long Matinee Collection' }, keywords: [1, 2], uhd: true,
  },
  {
    id: 102, title: 'Harbor Lights', year: 2019, date: '2019-06-14', minutes: 108, genres: [G.romance, G.drama], cert: 'PG', vote: 7.2,
    tagline: 'Every boat comes home.',
    overview: 'A night-ferry captain and the woman who runs the harbor café spend one summer pretending they are not in love, while the whole town keeps score.',
    cast: cast([8, 'Captain Elias Brandt'], [9, 'Rosa Vale'], [4, 'Old Mo'], [3, 'Tess']),
    crew: [[23, 'Director'], [21, 'Original Music Composer']],
    studios: ['Quayside Films'], countries: ['Portugal'],
  },
  {
    id: 103, title: 'Paper Moons', year: 2021, date: '2021-11-24', minutes: 92, genres: [G.animation, G.family, G.fantasy], cert: 'PG', vote: 7.8,
    tagline: 'Cut out for adventure.',
    overview: 'When the moon falls into her grandfather\'s print shop, a paper-cut girl has one night to fold it back into the sky.',
    cast: cast([5, 'Pip (voice)'], [10, 'Scissors (voice)'], [6, 'Grandpa Ink (voice)'], [7, 'The Moon (voice)']),
    crew: [[22, 'Director'], [21, 'Original Music Composer']],
    studios: ['Foldline Animation'], countries: ['Poland'], collection: { id: 901, name: 'Paper Moons Collection' }, kids: true,
  },
  {
    id: 110, title: 'Paper Moons 2', year: 2025, date: '2025-11-21', minutes: 95, genres: [G.animation, G.family, G.adventure], cert: 'PG', vote: 7.4, scene: 103, hue: 140,
    overview: 'Pip and Scissors follow a runaway comet through the pages of an old atlas.',
    cast: cast([5, 'Pip (voice)'], [10, 'Scissors (voice)'], [11, 'Atlas (voice)']),
    crew: [[22, 'Director']],
    studios: ['Foldline Animation'], countries: ['Poland'], collection: { id: 901, name: 'Paper Moons Collection' }, kids: true,
  },
  {
    id: 104, title: 'Northbound', year: 2016, date: '2016-02-05', minutes: 117, genres: [G.adventure, G.drama], cert: 'PG-13', vote: 7.0,
    overview: 'Two estranged brothers take the last train to the Arctic Circle to scatter their father\'s ashes, and miss their stop on purpose.',
    cast: cast([8, 'Jonas'], [12, 'Erik'], [11, 'The Conductor']),
    crew: [[23, 'Director']],
    studios: ['Polar Line'], countries: ['Sweden'],
  },
  {
    id: 105, title: 'Velvet Static', year: 2023, date: '2023-09-29', minutes: 101, genres: [G.scifi, G.thriller], cert: 'R', vote: 6.9,
    tagline: 'Stay tuned.',
    overview: 'A late-night TV host notices that her broadcast is being answered, one second before she speaks.',
    cast: cast([7, 'Kit Moreau'], [2, 'Dalton'], [11, 'Dr. Varga']),
    crew: [[23, 'Director'], [21, 'Original Music Composer']],
    studios: ['Neon Hour'], countries: ['United States'], keywords: [3],
  },
  {
    id: 106, title: 'A Quiet Orbit', year: 2018, date: '2018-04-20', minutes: 133, genres: [G.scifi, G.drama], cert: 'PG-13', vote: 7.6,
    overview: 'The last crew member on a research station around Saturn teaches herself to garden while she waits for a ship that may not come.',
    cast: cast([11, 'Commander Ilse Brandt'], [12, 'Mission Control'], [1, 'Signal']),
    crew: [[20, 'Director'], [21, 'Original Music Composer']],
    studios: ['Lantern Pictures'], countries: ['Austria', 'Norway'], keywords: [3],
  },
  {
    id: 107, title: 'The Projectionist', year: 2022, date: '2022-08-12', minutes: 98, genres: [G.drama, G.comedy], cert: 'PG', vote: 7.3,
    overview: 'A small-town cinema has one week left. Its projectionist decides to show every film it ever played, in order, and the town keeps showing up.',
    cast: cast([2, 'Arturo'], [3, 'Dee'], [4, 'Mayor Pell'], [5, 'Lucy']),
    crew: [[20, 'Director']],
    studios: ['Lantern Pictures'], countries: ['Chile'], keywords: [1],
  },
  {
    id: 108, title: 'Salt & Cedar', year: 2020, date: '2020-05-01', minutes: 112, genres: [G.romance, G.drama], cert: 'PG-13', vote: 6.8,
    overview: 'A carpenter rebuilding a cliff-top house and the architect who wants it torn down spend a stormy spring arguing about what is worth saving.',
    cast: cast([9, 'Wren'], [12, 'Aldous'], [6, 'Harlan']),
    crew: [[23, 'Director']],
    studios: ['Quayside Films'], countries: ['Canada'],
  },
  {
    id: 109, title: 'Last Reel', year: 2017, date: '2017-10-06', minutes: 104, genres: [G.western, G.thriller], cert: 'R', vote: 7.1,
    tagline: 'One road. One reel. One chance.',
    overview: 'A travelling projectionist crosses the desert with a film everyone wants and no one should see.',
    cast: cast([2, 'Cole Varga'], [9, 'Marisol'], [4, 'Sheriff Dunn']),
    crew: [[23, 'Director'], [21, 'Original Music Composer']],
    studios: ['Neon Hour'], countries: ['Mexico', 'United States'], collection: { id: 900, name: 'The Long Matinee Collection' },
  },
  {
    id: 111, title: 'Midnight Ferry', year: 2015, date: '2015-03-13', minutes: 96, genres: [G.mystery, G.thriller], cert: 'PG-13', vote: 6.6, scene: 102, hue: 150,
    overview: 'Seven passengers, one crossing, and a cabin that was empty when the ferry left port.',
    cast: cast([8, 'Inspector Hale'], [7, 'Mina'], [6, 'The Purser']),
    crew: [[23, 'Director']],
    studios: ['Quayside Films'], countries: ['United Kingdom'],
  },
  {
    id: 112, title: 'Glass Harbor', year: 2013, date: '2013-07-19', minutes: 89, genres: [G.comedy, G.romance], cert: 'PG', vote: 6.4, scene: 108, hue: 200,
    overview: 'A glassblower who breaks everything she touches falls for the insurance assessor sent to count the damage.',
    cast: cast([3, 'Greta'], [8, 'Fenwick']),
    crew: [[22, 'Director']],
    studios: ['Quayside Films'], countries: ['Ireland'],
  },
  {
    id: 113, title: 'The Kite Thief', year: 2012, date: '2012-04-06', minutes: 81, genres: [G.animation, G.family, G.comedy], cert: 'G', vote: 7.0, scene: 103, hue: 80,
    overview: 'A crow with a taste for kites steals the wrong one and ends up flying a whole village to the sea.',
    cast: cast([10, 'Crow (voice)'], [5, 'Ada (voice)']),
    crew: [[22, 'Director']],
    studios: ['Foldline Animation'], countries: ['Poland'], kids: true,
  },
  {
    id: 114, title: 'Static Bloom', year: 1986, date: '1986-06-27', minutes: 99, genres: [G.scifi, G.action], cert: 'PG-13', vote: 6.7, scene: 105, hue: 300,
    overview: 'In a neon city where the weather is broadcast, a repairman finds a station that plays tomorrow\'s storms.',
    cast: cast([6, 'Rook'], [11, 'Sable']),
    crew: [[23, 'Director']],
    studios: ['Neon Hour'], countries: ['United States'], keywords: [3],
  },
  {
    id: 115, title: 'Night Bus to Lisbon', year: 1994, date: '1994-09-09', minutes: 87, genres: [G.comedy], cert: 'PG-13', vote: 6.9, scene: 109, hue: 190,
    overview: 'A wedding band misses its flight and takes the overnight bus across three countries with a borrowed tuba and no sleep.',
    cast: cast([10, 'Benny'], [9, 'Lu'], [4, 'The Driver']),
    crew: [[22, 'Director']],
    studios: ['Quayside Films'], countries: ['Portugal', 'Spain'],
  },
  {
    id: 116, title: 'Little Lantern', year: 2021, date: '2021-12-10', minutes: 74, genres: [G.animation, G.family], cert: 'G', vote: 7.5, scene: 101, hue: 50,
    overview: 'A small lantern on a big ship learns that the dark is only the part of the sea no one has lit yet.',
    cast: cast([5, 'Lantern (voice)'], [6, 'Captain Gull (voice)']),
    crew: [[22, 'Director']],
    studios: ['Foldline Animation'], countries: ['Denmark'], kids: true,
  },
]

export const shows = [
  {
    id: 201, title: 'Crumb Street', year: 2022, date: '2022-01-14', genres: [TV.comedy, TV.drama], cert: 'TV-PG', vote: 8.1, status: 'Returning Series',
    overview: 'Two rival bakers inherit the same bakery on the same street, and neither will sell.',
    cast: cast([3, 'Pearl Anand'], [8, 'Hugo Thorne'], [4, 'Mr. Bastien'], [9, 'Odile']),
    creators: [22], networks: ['Lantern TV'],
    seasons: [
      ['Proofing', 'Dough Rises', 'The Flood', 'Sourdough Wars', 'Night Shift', 'Burnt Ends'],
      ['Second Rise', 'The Critic', 'Flour Power', 'Open House', 'Last Loaf', 'New Ovens'],
    ],
    episodeOverviews: ['The ovens fail on the busiest morning of the year.', 'Pearl hides a secret recipe in plain sight.', 'A burst pipe forces the rivals to share one kitchen.', 'The street votes on whose bread is better, and nobody wins.', 'Hugo takes the night shift and finds out who has been stealing croissants.', 'A wedding cake, two bakers, one oven.'],
  },
  {
    id: 202, title: 'Signal Fires', year: 2020, date: '2020-10-02', genres: [TV.drama, TV.mystery], cert: 'TV-14', vote: 8.4, status: 'Ended', ended: '2021-12-03',
    overview: 'In a mountain valley cut off by snow, the old beacons start lighting themselves, one each night, and the valley\'s only radio operator is the only one who knows what comes after the last.',
    cast: cast([1, 'Ingrid Solberg'], [12, 'Father Aris'], [7, 'Yuki'], [6, 'The Warden']),
    creators: [20], networks: ['North Channel'],
    seasons: [
      ['First Light', 'The Pass', 'Whiteout', 'Beacon Nine', 'Thaw'],
      ['Embers', 'The Valley Below', 'Old Frequencies', 'The Last Fire'],
    ],
    episodeOverviews: ['The first beacon lights on the night the pass closes.', 'A search party goes up the mountain and only half comes back.', 'Ingrid hears her own voice on the radio.', 'The village argues about putting the fires out.', 'Spring comes early, and with it, a stranger.'],
  },
  {
    id: 203, title: 'Tiny Robots', year: 2023, date: '2023-03-06', genres: [TV.kids, TV.animation, TV.comedy], cert: 'TV-Y', vote: 7.9, status: 'Returning Series', scene: 106, hue: 120, kids: true,
    overview: 'Bolt and Bit are two very small robots with very big ideas, living in the junk drawer of a very busy kitchen.',
    cast: cast([10, 'Bolt (voice)'], [5, 'Bit (voice)']),
    creators: [22], networks: ['Lantern Kids'],
    seasons: [['The Big Drawer', 'Spoon Day', 'Magnet Trouble', 'The Toaster Mountain', 'Lost Battery', 'Bedtime Beeps']],
    episodeOverviews: ['Bolt and Bit find out the drawer has a back.', 'Everything is a spoon today.', 'Bit gets stuck to the fridge.', 'The tallest thing in the kitchen is warm.'],
  },
]

export const albums = [
  { artist: 'Aurora Static', album: 'Signal Fires', year: 2021, scene: 202, tracks: ['Opening Credits', 'Velvet Seats', 'Intermission', 'Projector Hum', 'Snowline', 'Encore'] },
  { artist: 'The Midnight Reels', album: 'Late Show', year: 2019, scene: 105, tracks: ['Neon Marquee', 'Back Row', 'Usher\'s Waltz', 'Double Feature', 'Closing Time'] },
  { artist: 'Harbor Choir', album: 'Lights on the Water', year: 2023, scene: 102, tracks: ['Quay', 'Night Ferry', 'Lanterns', 'Tide Tables'] },
  { artist: 'Aurora Static', album: 'Night Signals', year: 2018, scene: 104, tracks: ['Dial Tone', 'Static Bloom', 'Last Broadcast', 'Antenna Song', 'Test Pattern'] },
  { artist: 'Paper Lanterns', album: 'Small Hours', year: 2022, scene: 106, hue: 40, tracks: ['Kettle', 'Porch Light', 'Small Hours', 'Fog Lamp'] },
  { artist: 'Cobalt Avenue', album: 'Drive-In', year: 2020, scene: 108, hue: 300, tracks: ['Headlights', 'Speaker Post', 'Second Feature', 'Tail Lights', 'Gravel'] },
  { artist: 'The Midnight Reels', album: 'Matinee Idols', year: 2024, scene: 103, hue: 200, tracks: ['Front Row', 'Cartoon Short', 'Popcorn Line', 'House Lights'] },
]

export const keywords = [
  { id: 1, name: 'cinema' },
  { id: 2, name: 'island' },
  { id: 3, name: 'broadcast' },
]

// Titles the demo library doesn't have, for the Discover tab (fake-overseerr.mjs).
// list: which of Overseerr's lists each is on; status: Overseerr's media
// status (2 asked for, 3 on its way).
export const discover = [
  { id: 301, kind: 'movie', title: 'The Cartographer\'s Daughter', date: '2025-09-12', vote: 7.9, scene: 107, hue: 30, lists: ['trending', 'popular-movies'], overview: 'A mapmaker\'s daughter finds an island on her father\'s last map that no ship has ever reached.' },
  { id: 302, kind: 'tv', title: 'Night Shift at the Planetarium', date: '2025-02-03', vote: 8.2, scene: 105, hue: 200, lists: ['trending', 'popular-shows'], seasons: 2, overview: 'The staff of a city planetarium keep the stars running after closing time, and the stars keep them company.' },
  { id: 303, kind: 'movie', title: 'Under the Pier', date: '2024-07-19', vote: 7.1, scene: 102, hue: 300, lists: ['trending', 'popular-movies'], status: 2, overview: 'Three cousins spend a summer looking for the treasure their grandmother swore she buried under the old pier.' },
  { id: 304, kind: 'movie', title: 'Copper Sky', date: '2025-05-30', vote: 7.6, scene: 108, hue: 60, lists: ['trending', 'popular-movies'], overview: 'In a desert town that runs on one radio station, the night DJ starts taking requests from someone who isn\'t there.' },
  { id: 305, kind: 'tv', title: 'The Lamplighters', date: '2024-10-11', vote: 8.0, scene: 116, hue: 250, lists: ['trending', 'popular-shows'], seasons: 3, overview: 'In the last city still lit by gas, a guild of lamplighters guards a secret older than electricity.' },
  { id: 306, kind: 'movie', title: 'Winter Orchard', date: '2023-12-08', vote: 7.4, scene: 109, hue: 160, lists: ['popular-movies'], overview: 'A family gathers at the apple farm for one last harvest before it\'s sold.' },
  { id: 307, kind: 'movie', title: 'Second Projector', date: '2025-01-24', vote: 6.9, scene: 104, hue: 90, lists: ['popular-movies', 'trending'], status: 3, overview: 'A small-town cinema gets a projector from a closed movie palace, and every film it shows ends differently.' },
  { id: 308, kind: 'tv', title: 'Lighthouse Kitchen', date: '2023-04-17', vote: 7.7, scene: 101, hue: 330, lists: ['popular-shows'], seasons: 4, overview: 'A chef takes over the restaurant at the end of the world\'s longest pier.' },
  { id: 309, kind: 'tv', title: 'Orbit Academy', date: '2024-09-02', vote: 7.5, scene: 106, hue: 40, lists: ['popular-shows', 'trending'], seasons: 2, overview: 'The first class of a school on a space station learns that homework is harder in zero gravity.' },
  { id: 310, kind: 'movie', title: 'The Tidewater Waltz', date: '2026-12-18', vote: 0, scene: 111, hue: 200, lists: ['upcoming-movies'], overview: 'Two rival dance-hall owners on a flooded boardwalk agree to one last dance-off.' },
  { id: 311, kind: 'movie', title: 'Lantern Season', date: '2026-11-26', vote: 0, scene: 116, hue: 20, lists: ['upcoming-movies', 'trending'], overview: 'A girl who builds paper lanterns sets one free on the wrong night and has to bring it back.' },
  { id: 312, kind: 'movie', title: 'Quiet Signals', date: '2027-02-12', vote: 0, scene: 202, hue: 120, lists: ['upcoming-movies'], overview: 'A radio astronomer hears a song in the static, and so does her daughter, three hundred miles away.' },
  { id: 313, kind: 'tv', title: 'Greenhouse', date: '2026-11-09', vote: 0, scene: 109, hue: 80, lists: ['upcoming-shows'], seasons: 1, overview: 'A botanist inherits a greenhouse where every plant is from a place that doesn\'t exist.' },
  { id: 314, kind: 'tv', title: 'The Ferry Years', date: '2027-01-15', vote: 0, scene: 111, hue: 280, lists: ['upcoming-shows'], seasons: 1, overview: 'Thirty years in the lives of the people who ride the same morning ferry.' },
  { id: 315, kind: 'movie', title: 'Beacon Road', date: '2022-08-26', vote: 7.3, scene: 202, hue: 0, lists: [], overview: 'A snowplow driver and a stranded musician cross a mountain pass the night the beacons go dark.' },
  { id: 316, kind: 'movie', title: 'The Last Ferry Out', date: '2021-05-07', vote: 7.0, scene: 111, hue: 60, lists: [], overview: 'The final crossing of a ferry line, and everyone who came to say goodbye.' },
  { id: 317, kind: 'tv', title: 'Reel Life', date: '2023-01-20', vote: 7.8, scene: 101, hue: 180, lists: [], seasons: 2, overview: 'A family of projectionists runs the oldest cinema in the county, one disaster at a time.' },
]

// What Overseerr recommends for a title in the demo library, by TMDB id.
export const recommendations = {
  101: [317, 315, 307, 304, 301, 116, 316, 305],
  102: [316, 308, 303, 306, 314, 301],
  201: [308, 317, 309, 302, 305, 306],
  default: [301, 304, 305, 309],
}
