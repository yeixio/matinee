// The README's demo library: real public-domain and Creative Commons films,
// cartoons, sheet music and books, with artwork from Wikimedia Commons (the
// same art Box Office's README uses; credits in docs/images/readme/CREDITS.md).
// catalog.mjs swaps these in for its made-up titles when
// MATINEE_DEMO_LIBRARY=public, which `pnpm shots:readme` sets.
//
// art: the file's name on Wikimedia Commons.

const G = { action: 28, adventure: 12, animation: 16, comedy: 35, crime: 80, drama: 18, family: 10751, fantasy: 14, horror: 27, mystery: 9648, romance: 10749, scifi: 878, thriller: 53 }
const TV = { kids: 10762, scififantasy: 10765, comedy: 35, animation: 16, action: 10759 }

// Real people, by name only: no photos, birthdays or biographies.
const names = [
  'Sacha Goedegebure', 'Colin Levy', 'Halina Reijn', 'Thom Hoffman', 'Ian Hubert', 'Derek de Lint', 'Sergio Hasselbaink', 'Rogier Schippers', 'Vanja Rukavina',
  'George A. Romero', 'Duane Jones', 'Judith O\'Dea', 'Karl Hardman', 'Marilyn Eastman', 'Keith Wayne',
  'Howard Hawks', 'Cary Grant', 'Rosalind Russell', 'Ralph Bellamy', 'Gene Lockhart',
  'Stanley Donen', 'Audrey Hepburn', 'Walter Matthau', 'James Coburn', 'George Kennedy', 'Henry Mancini',
  'Buster Keaton', 'Clyde Bruckman', 'Marion Mack', 'Glen Cavender', 'Kathryn McGuire', 'Ward Crane',
  'Harold Lloyd', 'Mildred Davis', 'Bill Strother', 'Fred C. Newmeyer', 'Sam Taylor',
  'Rupert Julian', 'Lon Chaney', 'Mary Philbin', 'Norman Kerry',
  'Robert Wiene', 'Werner Krauss', 'Conrad Veidt', 'Lil Dagover', 'Friedrich Fehér',
  'Edgar G. Ulmer', 'Tom Neal', 'Ann Savage', 'Claudia Drake',
  'Fritz Lang', 'Brigitte Helm', 'Gustav Fröhlich', 'Alfred Abel', 'Rudolf Klein-Rogge',
  'Otto Messmer', 'Pat Sullivan',
]
export const people = names.map((name, i) => ({ id: i + 1, name, bio: '', photo: false }))
const who = (name) => {
  const p = people.find((x) => x.name === name)
  if (!p) throw new Error(`no such person: ${name}`)
  return p.id
}
const cast = (...list) => list.map(([name, character], order) => ({ id: who(name), character, order }))
const crew = (...list) => list.map(([name, job]) => [who(name), job])

export const movies = [
  {
    id: 1001, title: 'Big Buck Bunny', year: 2008, date: '2008-05-30', minutes: 10, genres: [G.animation, G.comedy, G.family], cert: 'G', vote: 6.4, art: 'Big_buck_bunny_poster_big.jpg',
    tagline: 'A big bunny with a big heart.',
    overview: 'A large and lovable rabbit deals with three tiny bullies, led by a flying squirrel, who are determined to squelch his happiness.',
    cast: [], crew: crew(['Sacha Goedegebure', 'Director']), studios: ['Blender Foundation'], countries: ['Netherlands'], kids: true, keywords: [1],
  },
  {
    id: 1002, title: 'Sintel', year: 2010, date: '2010-09-27', minutes: 15, genres: [G.animation, G.fantasy, G.adventure], cert: 'PG', vote: 7.4, art: 'Sintel_poster.jpg',
    overview: 'A lonely young woman, Sintel, helps and befriends a dragon, whom she calls Scales. But when he is kidnapped by an adult dragon, Sintel decides to embark on a dangerous quest to find her lost friend.',
    cast: cast(['Halina Reijn', 'Sintel (voice)'], ['Thom Hoffman', 'Shaman (voice)']), crew: crew(['Colin Levy', 'Director']), studios: ['Blender Foundation'], countries: ['Netherlands'], keywords: [1],
  },
  {
    id: 1003, title: 'Tears of Steel', year: 2012, date: '2012-09-26', minutes: 12, genres: [G.scifi, G.action], cert: 'PG-13', vote: 6.2, art: 'Tos-poster.png',
    overview: 'In an apocalyptic future, a group of soldiers and scientists takes refuge in Amsterdam to try to stop an army of robots that threatens the planet.',
    cast: cast(['Derek de Lint', 'Old Thom'], ['Sergio Hasselbaink', 'Barley'], ['Rogier Schippers', 'Captain'], ['Vanja Rukavina', 'Celia']), crew: crew(['Ian Hubert', 'Director']),
    studios: ['Blender Foundation'], countries: ['Netherlands'], keywords: [1],
  },
  {
    id: 1004, title: 'Night of the Living Dead', year: 1968, date: '1968-10-04', minutes: 96, genres: [G.horror, G.thriller], cert: 'R', vote: 7.5, art: 'Night_Of_The_Living_Dead_(1968)_-_Poster.jpg',
    tagline: 'They won\'t stay dead!',
    overview: 'A group of strangers barricade themselves in a farmhouse in rural Pennsylvania as the recently dead rise and lay siege to it.',
    cast: cast(['Duane Jones', 'Ben'], ['Judith O\'Dea', 'Barbra'], ['Karl Hardman', 'Harry Cooper'], ['Marilyn Eastman', 'Helen Cooper'], ['Keith Wayne', 'Tom']),
    crew: crew(['George A. Romero', 'Director'], ['George A. Romero', 'Screenplay']), studios: ['Image Ten'], countries: ['United States'], uhd: true,
  },
  {
    id: 1005, title: 'His Girl Friday', year: 1940, date: '1940-01-18', minutes: 92, genres: [G.comedy, G.romance], cert: 'NR', vote: 7.6, art: 'His_Girl_Friday_(1940_poster)_crop.jpg',
    overview: 'A newspaper editor uses every trick in the book to keep his ace reporter and ex-wife from remarrying, and to get her to cover one last story.',
    cast: cast(['Cary Grant', 'Walter Burns'], ['Rosalind Russell', 'Hildy Johnson'], ['Ralph Bellamy', 'Bruce Baldwin'], ['Gene Lockhart', 'Sheriff Hartwell']),
    crew: crew(['Howard Hawks', 'Director']), studios: ['Columbia Pictures'], countries: ['United States'],
  },
  {
    id: 1006, title: 'Charade', year: 1963, date: '1963-12-05', minutes: 113, genres: [G.comedy, G.mystery, G.romance, G.thriller], cert: 'NR', vote: 7.7, art: 'Charade_(1963_poster).jpg',
    tagline: 'You can expect the unexpected.',
    overview: 'Romance and suspense in Paris, as a woman is pursued by several men who want a fortune her murdered husband had stolen. Whom can she trust?',
    cast: cast(['Cary Grant', 'Peter Joshua'], ['Audrey Hepburn', 'Regina Lampert'], ['Walter Matthau', 'Hamilton Bartholomew'], ['James Coburn', 'Tex Panthollow'], ['George Kennedy', 'Herman Scobie']),
    crew: crew(['Stanley Donen', 'Director'], ['Henry Mancini', 'Original Music Composer']), studios: ['Universal Pictures'], countries: ['United States'], collection: { id: 900, name: 'Cary Grant Comedies' },
  },
  {
    id: 1007, title: 'The General', year: 1926, date: '1926-12-31', minutes: 79, genres: [G.action, G.adventure, G.comedy], cert: 'NR', vote: 8.0, art: 'The_General_(1926)_-_Movie_Poster_2.png',
    overview: 'When Union spies steal an engineer\'s beloved locomotive, he pursues it single-handedly, and straight through enemy lines.',
    cast: cast(['Buster Keaton', 'Johnnie Gray'], ['Marion Mack', 'Annabelle Lee'], ['Glen Cavender', 'Captain Anderson']),
    crew: crew(['Buster Keaton', 'Director'], ['Clyde Bruckman', 'Director']), studios: ['Buster Keaton Productions'], countries: ['United States'], collection: { id: 901, name: 'Buster Keaton Collection' },
  },
  {
    id: 1008, title: 'Safety Last!', year: 1923, date: '1923-04-01', minutes: 70, genres: [G.comedy], cert: 'NR', vote: 7.9, art: 'Safety_last_poster.jpg',
    overview: 'A boy from a small town moves to the big city to make good, and ends up climbing the outside of a department store to prove it.',
    cast: cast(['Harold Lloyd', 'The Boy'], ['Mildred Davis', 'The Girl'], ['Bill Strother', 'The Pal']),
    crew: crew(['Fred C. Newmeyer', 'Director'], ['Sam Taylor', 'Director']), studios: ['Hal Roach Studios'], countries: ['United States'],
  },
  {
    id: 1009, title: 'The Phantom of the Opera', year: 1925, date: '1925-11-15', minutes: 93, genres: [G.horror, G.drama], cert: 'NR', vote: 7.2, art: 'The_Phantom_of_the_Opera_(1925_film).jpg',
    overview: 'A mad, disfigured composer seeks love with a lovely young opera singer, from the cellars beneath the Paris Opera House.',
    cast: cast(['Lon Chaney', 'The Phantom'], ['Mary Philbin', 'Christine Daaé'], ['Norman Kerry', 'Raoul de Chagny']),
    crew: crew(['Rupert Julian', 'Director']), studios: ['Universal Pictures'], countries: ['United States'],
  },
  {
    id: 1010, title: 'Sherlock Jr.', year: 1924, date: '1924-04-21', minutes: 45, genres: [G.comedy, G.action], cert: 'NR', vote: 8.0, art: 'Sherlock_jr_poster.jpg',
    overview: 'A film projectionist longs to be a detective, and puts his meagre skills to work when he is framed by a rival for stealing his girlfriend\'s father\'s pocketwatch.',
    cast: cast(['Buster Keaton', 'Projectionist'], ['Kathryn McGuire', 'The Girl'], ['Ward Crane', 'The Sheik']),
    crew: crew(['Buster Keaton', 'Director']), studios: ['Buster Keaton Productions'], countries: ['United States'], collection: { id: 901, name: 'Buster Keaton Collection' },
  },
  {
    id: 1011, title: 'The Cabinet of Dr. Caligari', year: 1920, date: '1920-02-26', minutes: 77, genres: [G.horror, G.mystery], cert: 'NR', vote: 7.8, art: 'The_Cabinet_of_Doctor_Caligari_Movie_poster.jpg',
    overview: 'A hypnotist uses a sleepwalker to commit murders in a small German town, in a story told by a young man in a garden.',
    cast: cast(['Werner Krauss', 'Dr. Caligari'], ['Conrad Veidt', 'Cesare'], ['Friedrich Fehér', 'Francis'], ['Lil Dagover', 'Jane Olsen']),
    crew: crew(['Robert Wiene', 'Director']), studios: ['Decla-Bioscop'], countries: ['Germany'],
  },
  {
    id: 1012, title: 'Detour', year: 1945, date: '1945-11-30', minutes: 68, genres: [G.crime, G.drama, G.thriller], cert: 'NR', vote: 6.9, art: 'Detour_(poster).jpg',
    overview: 'A nightclub pianist hitchhikes across the country to join his girlfriend in Los Angeles, and a ride with the wrong man changes everything.',
    cast: cast(['Tom Neal', 'Al Roberts'], ['Ann Savage', 'Vera'], ['Claudia Drake', 'Sue Harvey']),
    crew: crew(['Edgar G. Ulmer', 'Director']), studios: ['Producers Releasing Corporation'], countries: ['United States'],
  },
  {
    id: 1013, title: 'Metropolis', year: 1927, date: '1927-01-10', minutes: 153, genres: [G.scifi, G.drama], cert: 'NR', vote: 8.1, art: 'Metropolis_(1926_US_ad_sheet_-_Paramount).jpg',
    tagline: 'There can be no understanding between the hands and the brain unless the heart acts as mediator.',
    overview: 'In a futuristic city sharply divided between the working class and the city planners, the son of the city\'s mastermind falls in love with a working-class prophet who predicts the coming of a savior.',
    cast: cast(['Brigitte Helm', 'Maria'], ['Gustav Fröhlich', 'Freder'], ['Alfred Abel', 'Joh Fredersen'], ['Rudolf Klein-Rogge', 'Rotwang']),
    crew: crew(['Fritz Lang', 'Director']), studios: ['UFA'], countries: ['Germany'], keywords: [2],
  },
]

export const shows = [
  {
    id: 2002, title: 'Felix the Cat', year: 1922, date: '1922-01-01', genres: [TV.kids, TV.animation, TV.comedy], cert: 'TV-G', vote: 7.1, status: 'Ended', ended: '1925-12-31', art: 'Felix_in_the_Swim_Title_Card.png', kids: true,
    overview: 'The silent cartoon adventures of a clever black cat who solves every problem with whatever is at hand, including his own tail.',
    cast: [], creators: [who('Otto Messmer')], networks: ['Pat Sullivan Studio'],
    seasons: [['Felix Saves the Day', 'Felix in the Swim', 'Felix Turns the Tide', 'Felix in Hollywood', 'Felix Trifles with Time']],
    episodeOverviews: ['Felix finds a way into the ballgame.', 'Felix wants to go swimming, but the piano lesson comes first.', 'Felix runs away to sea.', 'Felix heads west to try his luck in the pictures.', 'Felix finds Father Time and goes back to the Stone Age.'],
  },
]

// Scott Joplin's rags, as albums of his sheet music.
export const albums = [
  { artist: 'Scott Joplin', album: 'Maple Leaf Rag', label: 'John Stark & Son', year: 1899, art: 'Maple_Leaf_Rag_1st_ed.jpg', tracks: ['Maple Leaf Rag', 'Original Rags', 'Swipesy', 'Sunflower Slow Drag', 'Peacherine Rag'] },
  { artist: 'Scott Joplin', album: 'The Entertainer', label: 'John Stark & Son', year: 1902, art: 'Entertainer_1.jpg', tracks: ['The Entertainer', 'The Easy Winners', 'A Breeze from Alabama', 'Cleopha', 'March Majestic'] },
  { artist: 'Scott Joplin', album: 'Elite Syncopations', label: 'John Stark & Son', year: 1902, art: 'Elite_Syncopations_1.jpg', tracks: ['Elite Syncopations', 'The Strenuous Life', 'The Ragtime Dance', 'Palm Leaf Rag'] },
  { artist: 'Scott Joplin', album: 'Pine Apple Rag', label: 'Seminary Music', year: 1908, art: 'PineAppleRagCover08.jpg', tracks: ['Pine Apple Rag', 'Wall Street Rag', 'Solace', 'Gladiolus Rag', 'Euphonic Sounds'] },
  { artist: 'Scott Joplin', album: 'Treemonisha', label: 'Scott Joplin', year: 1911, art: 'Treemonisha_cover.jpg', tracks: ['Overture', 'The Bag of Luck', 'We\'re Goin\' Around', 'Aunt Dinah Has Blowed de Horn', 'A Real Slow Drag'] },
]

// Sherlock Holmes, read aloud: one file with chapters inside, or a folder
// with a file per chapter, as catalog.mjs's books are.
export const audiobooks = [
  {
    author: 'Arthur Conan Doyle', series: 'Sherlock Holmes', index: 1, title: 'A Study in Scarlet', narrator: 'Volunteer readers', year: 1887, art: '"A_Study_in_Scarlet"_in_Beeton’s_Christmas_Annual_(1887).jpg', whole: true,
    overview: 'Dr. Watson meets Sherlock Holmes, takes rooms with him at 221B Baker Street, and watches him solve a murder in a deserted house off the Brixton Road.',
    chapters: ['Mr. Sherlock Holmes', 'The Science of Deduction', 'The Lauriston Garden Mystery', 'What John Rance Had to Tell', 'Our Advertisement Brings a Visitor', 'Tobias Gregson Shows What He Can Do', 'Light in the Darkness'],
  },
  {
    author: 'Arthur Conan Doyle', series: 'Sherlock Holmes', index: 2, title: 'The Sign of the Four', narrator: 'Volunteer readers', year: 1890, art: 'Arthur_Conan_Doyle_-_The_Sign_of_Four,_cover_1892.jpg', whole: false,
    overview: 'A young woman receives a pearl in the post every year, and then a letter asking to meet. Holmes and Watson follow her into a story of stolen treasure.',
    chapters: ['The Science of Deduction', 'The Statement of the Case', 'In Quest of a Solution', 'The Story of the Bald-Headed Man', 'The Tragedy of Pondicherry Lodge', 'Sherlock Holmes Gives a Demonstration'],
  },
  {
    author: 'Arthur Conan Doyle', series: 'Sherlock Holmes', index: 3, title: 'The Adventures of Sherlock Holmes', narrator: 'Volunteer readers', year: 1892, art: 'Adventures_of_sherlock_holmes.jpg', whole: true,
    overview: 'Twelve of Holmes\'s cases, from a king\'s indiscretion to a league of red-headed men.',
    chapters: ['A Scandal in Bohemia', 'The Red-Headed League', 'A Case of Identity', 'The Boscombe Valley Mystery', 'The Five Orange Pips', 'The Man with the Twisted Lip'],
  },
  {
    author: 'Arthur Conan Doyle', series: 'Sherlock Holmes', index: 5, title: 'The Hound of the Baskervilles', narrator: 'Volunteer readers', year: 1902, art: 'Cover_(Hound_of_Baskervilles,_1902).jpg', whole: true,
    overview: 'A family curse, a gigantic hound on the moor, and a new heir to Baskerville Hall who may be next.',
    chapters: ['Mr. Sherlock Holmes', 'The Curse of the Baskervilles', 'The Problem', 'Sir Henry Baskerville', 'Three Broken Threads', 'Baskerville Hall'],
  },
]

export const keywords = [
  { id: 1, name: 'open movie' },
  { id: 2, name: 'robot' },
]

export const recommendations = { default: [301, 304, 305, 309] }

/** Every Commons file the library uses, for downloading. */
export const commonsFiles = [...new Set([...movies, ...shows, ...albums, ...audiobooks].map((t) => t.art))]
