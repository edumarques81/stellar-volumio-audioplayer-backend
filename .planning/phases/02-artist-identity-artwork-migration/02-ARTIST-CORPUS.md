# Live Artist Corpus — captured 2026-08-12 from MPD on stellar.local

**Build ARTIST-02's table-driven tests from THESE values, not invented ones.**

- `mpc list artist` → **124** distinct values
- `mpc list albumartist` → **50** distinct values

Values are shown verbatim inside backticks. Where a value contains a **double space**,
it is flagged — whitespace is not reliably single and a naive split on ' ' will misbehave.

## The collapse targets (multi-credit values)

| # | Raw value | Expected collapse |
|---|---|---|
| 1 | *(empty value)* | must not render a blank row (ARTIST-03) |
| 2 | `Adderley - Coltrane - Chambers - Cobb - Kelly` | `Adderley` |
| 3 | `Duke Ellington, John Coltrane` | `Duke Ellington` |
| 4 | `Ella Fitzgerald - vocals  Paul Smith - piano` ⚠ double-space | `Ella Fitzgerald` |
| 5 | `Ella Fitzgerald with Nelson Riddle And His Orchestra` | `Ella Fitzgerald` |
| 6 | `Herbert von Karajan  Wiener Philharmoniker` ⚠ double-space | `Herbert von Karajan` |
| 7 | `Jascha Horenstein - London Symphony Orchestra` | `Jascha Horenstein` |
| 8 | `Jascha Horenstein - Royal Philharmonic Orchestra` | `Jascha Horenstein` |
| 9 | `Luciano Pavarotti, Berliner Philharmoniker, Herbert von Karajan` | `Luciano Pavarotti` |
| 10 | `Luciano Pavarotti, Coro del Teatro Comunale di Bologna, Orchestra del Teatro Comunale di Bologna, Anton Guadagno` | `Luciano Pavarotti` |
| 11 | `Luciano Pavarotti, English Chamber Orchestra, Richard Bonynge` | `Luciano Pavarotti` |
| 12 | `Luciano Pavarotti, John Alldis Choir, Wandsworth School Boys Choir, London Philharmonic Orchestra, Zubin Mehta` | `Luciano Pavarotti` |
| 13 | `Luciano Pavarotti, London Symphony Orchestra, Richard Bonynge` | `Luciano Pavarotti` |
| 14 | `Luciano Pavarotti, Mirella Freni, Berliner Philharmoniker, Herbert von Karajan` | `Luciano Pavarotti` |
| 15 | `Luciano Pavarotti, National Philharmonic Orchestra, Giancarlo Chiaramello` | `Luciano Pavarotti` |
| 16 | `Luciano Pavarotti, National Philharmonic Orchestra, Nicola Rescigno` | `Luciano Pavarotti` |
| 17 | `Luciano Pavarotti, New Philharmonia Orchestra, Richard Bonynge` | `Luciano Pavarotti` |
| 18 | `Luciano Pavarotti, Nicolai Ghiaurov, National Philharmonic Orchestra, Robin Stapleton` | `Luciano Pavarotti` |
| 19 | `Luciano Pavarotti, Orchestra del Teatro Comunale di Bologna, Richard Bonynge` | `Luciano Pavarotti` |
| 20 | `Luciano Pavarotti, Orchestra of the Royal Opera House, Covent Garden, Edward Downes` | `Luciano Pavarotti` |
| 21 | `Luciano Pavarotti, Philharmonia Orchestra, Piero Gamba` | `Luciano Pavarotti` |
| 22 | `Luciano Pavarotti, Wiener Opernorchester, Nicola Rescigno` | `Luciano Pavarotti` |
| 23 | `Luciano Pavarotti, Wiener Philharmoniker, Sir Georg Solti` | `Luciano Pavarotti` |
| 24 | `Luciano Pavarotti, Wiener Volksopernorchester, Leone Magiera` | `Luciano Pavarotti` |
| 25 | `Miles Davis - Arranged and Directed by Gil Evans` | `Miles Davis` |
| 26 | `Moby, Apollo Jane, Deitrick Haddon, Mindy Jones` | `Moby` |
| 27 | `Moby, Gregory Porter, Amythyst Kiah` | `Moby` |
| 28 | `Moby, Jim James` | `Moby` |
| 29 | `Moby, Mark Lanegan, Kris Kristofferson` | `Moby` |
| 30 | `Moby, Mindy Jones` | `Moby` |
| 31 | `Moby, Nataly Dawn, Alice Skye, Luna Li` | `Moby` |
| 32 | `Moby, Novo Amor, Mindy Jones, Darlingside` | `Moby` |
| 33 | `Moby, Skylar Grey, Darlingside` | `Moby` |
| 34 | `Moby, Víkingur Ólafsson` | `Moby` |
| 35 | `Nat King Cole - with orchestra conducted by Billy May` | `Nat King Cole` |
| 36 | `Seiji Ozawa, Saito Kinen Orchestra` | `Seiji Ozawa` |

**36 of 124 values need collapsing.** Note 15 distinct `Luciano Pavarotti, …`
rows must all become one `Luciano Pavarotti`.

## Full Artist list (verbatim)

- ``  ← EMPTY VALUE
- `Adderley - Coltrane - Chambers - Cobb - Kelly`
- `Aldo Frank`
- `Andrea Parisy`
- `Ann Sorel`
- `Annie Ross`
- `Anomalie`
- `Artie Shaw`
- `Audrey Arno`
- `Benny Goodman`
- `Bert Ambrose`
- `Billie Eilish`
- `Billie Holiday`
- `Billy Nencioli`
- `Black Sabbath`
- `Bob Haggart`
- `Bobby Hackett`
- `Bunny Berigan`
- `Carmen Cavallero`
- `Cassius Lambert`
- `Charles Mingus`
- `Charles level`
- `Chet Baker`
- `Chick Webb`
- `Christiane Legrand`
- `Clarinha`
- `Coleman Hawkins`
- `Creedence Clearwater Revival`
- `Dick Powell`
- `Django Reinhardt`
- `Dooley Wilson`
- `Duke Ellington`
- `Duke Ellington, John Coltrane`
- `Dénes Varjon`
- `Edmond Hall`
- `Eduardo Marques`
- `Ella Fitzgerald - vocals  Paul Smith - piano` ⚠ double-space
- `Ella Fitzgerald with Nelson Riddle And His Orchestra`
- `Erroll Garner`
- `Festen`
- `Francoise Legrand`
- `Frank Gerard`
- `Frank Trumbauer`
- `Fritz Reiner Chicago Symphony`
- `Gaspar Claus`
- `Glenn Miller`
- `Harry James`
- `Herbert von Karajan  Wiener Philharmoniker` ⚠ double-space
- `Hit Parade des Enfants`
- `Isabelle Aubret`
- `Isabelle De Funes`
- `Jacob Collier`
- `Jascha Horenstein - London Symphony Orchestra`
- `Jascha Horenstein - Royal Philharmonic Orchestra`
- `Jean Constantin`
- `Jean-Pierre Lang`
- `Jean-Pierre Sabar`
- `John Coltrane And Johnny Hartman`
- `Jude Kofie`
- `Leonard Bernstein`
- `Les Masques`
- `Louis Armstrong`
- `Luciano Pavarotti, Berliner Philharmoniker, Herbert von Karajan`
- `Luciano Pavarotti, Coro del Teatro Comunale di Bologna, Orchestra del Teatro Comunale di Bologna, Anton Guadagno`
- `Luciano Pavarotti, English Chamber Orchestra, Richard Bonynge`
- `Luciano Pavarotti, John Alldis Choir, Wandsworth School Boys Choir, London Philharmonic Orchestra, Zubin Mehta`
- `Luciano Pavarotti, London Symphony Orchestra, Richard Bonynge`
- `Luciano Pavarotti, Mirella Freni, Berliner Philharmoniker, Herbert von Karajan`
- `Luciano Pavarotti, National Philharmonic Orchestra, Giancarlo Chiaramello`
- `Luciano Pavarotti, National Philharmonic Orchestra, Nicola Rescigno`
- `Luciano Pavarotti, New Philharmonia Orchestra, Richard Bonynge`
- `Luciano Pavarotti, Nicolai Ghiaurov, National Philharmonic Orchestra, Robin Stapleton`
- `Luciano Pavarotti, Orchestra del Teatro Comunale di Bologna, Richard Bonynge`
- `Luciano Pavarotti, Orchestra of the Royal Opera House, Covent Garden, Edward Downes`
- `Luciano Pavarotti, Philharmonia Orchestra, Piero Gamba`
- `Luciano Pavarotti, Wiener Opernorchester, Nicola Rescigno`
- `Luciano Pavarotti, Wiener Philharmoniker, Sir Georg Solti`
- `Luciano Pavarotti, Wiener Volksopernorchester, Leone Magiera`
- `Magalie Noël`
- `Maria Callas`
- `Mark Knopfler`
- `Marpessa Dawn`
- `Miles Davis & company`
- `Miles Davis - Arranged and Directed by Gil Evans`
- `Moby`
- `Moby, Apollo Jane, Deitrick Haddon, Mindy Jones`
- `Moby, Gregory Porter, Amythyst Kiah`
- `Moby, Jim James`
- `Moby, Mark Lanegan, Kris Kristofferson`
- `Moby, Mindy Jones`
- `Moby, Nataly Dawn, Alice Skye, Luna Li`
- `Moby, Novo Amor, Mindy Jones, Darlingside`
- `Moby, Skylar Grey, Darlingside`
- `Moby, Víkingur Ólafsson`
- `Nat King Cole`
- `Nat King Cole - with orchestra conducted by Billy May`
- `New York Philharmonic`
- `Norah Jones`
- `Olivia Dean`
- `Pittsburgh Symphony Orchestra; Manfred Honeck`
- `Pittsburgh Symphony Orchestra; Manfred Honeck; Christina Landshamer; Jennifer Johnson Cano; Werner Güra; Shenyang; Mendelssohn Choir of Pittsburgh`
- `Queens Of The Stone Age`
- `Quintet of the Hot Club de France`
- `Red`
- `Royal Concertgebouw Orchestra`
- `Seiji Ozawa, Saito Kinen Orchestra`
- `Sidney Bechet`
- `Simon Denizart`
- `Skeewiff`
- `Snarky Puppy`
- `Sophia Loren`
- `Stan Getz and João Gilberto featuring Antonio Carlos Jobim`
- `Stevie Ray Vaughan & Double Trouble`
- `Sylvia Fels`
- `Test Signals`
- `The Con Tempo`
- `The Dave Brubeck Quartet`
- `The Harlem Footwarmers`
- `The Tommy Dorsey Orchestra`
- `Them Crooked Vultures`
- `Tommy Dorsey`
- `Various Interprets`
- `Wilbur de Paris`
- `toe`

## Full AlbumArtist list (verbatim)

- ``  ← EMPTY VALUE
- `Adderley - Coltrane - Chambers - Cobb - Kelly`
- `Anomalie`
- `Billie Eilish`
- `Black Sabbath`
- `Cassius Lambert`
- `Charles Mingus`
- `Chet Baker`
- `Creedence Clearwater Revival`
- `Duke Ellington, John Coltrane`
- `Dénes Varjon`
- `Eduardo Marques`
- `Ella Fitzgerald - vocals  Paul Smith - piano` ⚠ double-space
- `Ella Fitzgerald with Nelson Riddle And His Orchestra`
- `Festen`
- `Fritz Reiner Chicago Symphony`
- `Gaspar Claus`
- `Herbert von Karajan`
- `Jacob Collier`
- `Jascha Horenstein - London Symphony Orchestra`
- `Jascha Horenstein - Royal Philharmonic Orchestra`
- `John Coltrane And Johnny Hartman`
- `Jude Kofie`
- `Luciano Pavarotti`
- `Maria Callas, Victor De Sabata, Orchestra del Teatro della Scala di Milano`
- `Mark Knopfler`
- `Miles Davis & company`
- `Miles Davis - Arranged and Directed by Gil Evans`
- `Moby`
- `Multi-interprètes`
- `Nat King Cole`
- `Nat King Cole - with orchestra conducted by Billy May`
- `Norah Jones`
- `Olivia Dean`
- `Pittsburgh Symphony Orchestra; Manfred Honeck`
- `Pittsburgh Symphony Orchestra; Manfred Honeck; Christina Landshamer; Jennifer Johnson Cano; Werner Güra; Shenyang; Mendelssohn Choir of Pittsburgh`
- `Queens Of The Stone Age`
- `Royal Concertgebouw Orchestra, New York Philharmonic, Wiener Philharmonic Orchestra, Leonard Bernstein`
- `Seiji Ozawa, Saito Kinen Orchestra`
- `Simon Denizart`
- `Skeewiff`
- `Snarky Puppy`
- `Snarky Puppy, Metropole Orkest`
- `Stan Getz and João Gilberto featuring Antonio Carlos Jobim`
- `Stevie Ray Vaughan & Double Trouble`
- `Test Signals`
- `The Dave Brubeck Quartet`
- `Them Crooked Vultures`
- `Various Artists`
- `toe`
