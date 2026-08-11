package artistidentity

import "testing"

// TestCollapse_Corpus is built from the REAL 124-value artist corpus captured
// live from MPD on stellar.local on 2026-08-12
// (.planning/phases/02-artist-identity-artwork-migration/02-ARTIST-CORPUS.md),
// not invented examples. 36 of the 124 raw values are known multi-credit
// values that must collapse to a single canonical first-credited performer
// (per the corpus's numbered "collapse targets" table, including all 15
// distinct `Luciano Pavarotti, ...` variants collapsing to one
// `Luciano Pavarotti` and all 9 `Moby, ...` variants collapsing to one
// `Moby`); the other 88 values -- including ones joined by `&`, `;`,
// capitalized `And`, lowercase `and`/`featuring`, or a bare hyphen with no
// surrounding spaces (e.g. `Jean-Pierre Lang`) -- must pass through
// byte-for-byte unchanged. Getting the 88 "must not collapse" cases wrong is
// as bad as failing to collapse the 36 that must.
func TestCollapse_Corpus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{`empty value (ARTIST-03)`, ``, ``},
		{`corpus[002] Adderley - Coltrane - Chambers - Cobb - `, `Adderley - Coltrane - Chambers - Cobb - Kelly`, `Adderley`},
		{`corpus[003] Aldo Frank`, `Aldo Frank`, `Aldo Frank`},
		{`corpus[004] Andrea Parisy`, `Andrea Parisy`, `Andrea Parisy`},
		{`corpus[005] Ann Sorel`, `Ann Sorel`, `Ann Sorel`},
		{`corpus[006] Annie Ross`, `Annie Ross`, `Annie Ross`},
		{`corpus[007] Anomalie`, `Anomalie`, `Anomalie`},
		{`corpus[008] Artie Shaw`, `Artie Shaw`, `Artie Shaw`},
		{`corpus[009] Audrey Arno`, `Audrey Arno`, `Audrey Arno`},
		{`corpus[010] Benny Goodman`, `Benny Goodman`, `Benny Goodman`},
		{`corpus[011] Bert Ambrose`, `Bert Ambrose`, `Bert Ambrose`},
		{`corpus[012] Billie Eilish`, `Billie Eilish`, `Billie Eilish`},
		{`corpus[013] Billie Holiday`, `Billie Holiday`, `Billie Holiday`},
		{`corpus[014] Billy Nencioli`, `Billy Nencioli`, `Billy Nencioli`},
		{`corpus[015] Black Sabbath`, `Black Sabbath`, `Black Sabbath`},
		{`corpus[016] Bob Haggart`, `Bob Haggart`, `Bob Haggart`},
		{`corpus[017] Bobby Hackett`, `Bobby Hackett`, `Bobby Hackett`},
		{`corpus[018] Bunny Berigan`, `Bunny Berigan`, `Bunny Berigan`},
		{`corpus[019] Carmen Cavallero`, `Carmen Cavallero`, `Carmen Cavallero`},
		{`corpus[020] Cassius Lambert`, `Cassius Lambert`, `Cassius Lambert`},
		{`corpus[021] Charles Mingus`, `Charles Mingus`, `Charles Mingus`},
		{`corpus[022] Charles level`, `Charles level`, `Charles level`},
		{`corpus[023] Chet Baker`, `Chet Baker`, `Chet Baker`},
		{`corpus[024] Chick Webb`, `Chick Webb`, `Chick Webb`},
		{`corpus[025] Christiane Legrand`, `Christiane Legrand`, `Christiane Legrand`},
		{`corpus[026] Clarinha`, `Clarinha`, `Clarinha`},
		{`corpus[027] Coleman Hawkins`, `Coleman Hawkins`, `Coleman Hawkins`},
		{`corpus[028] Creedence Clearwater Revival`, `Creedence Clearwater Revival`, `Creedence Clearwater Revival`},
		{`corpus[029] Dick Powell`, `Dick Powell`, `Dick Powell`},
		{`corpus[030] Django Reinhardt`, `Django Reinhardt`, `Django Reinhardt`},
		{`corpus[031] Dooley Wilson`, `Dooley Wilson`, `Dooley Wilson`},
		{`corpus[032] Duke Ellington`, `Duke Ellington`, `Duke Ellington`},
		{`corpus[033] Duke Ellington, John Coltrane`, `Duke Ellington, John Coltrane`, `Duke Ellington`},
		{`corpus[034] Dénes Varjon`, `Dénes Varjon`, `Dénes Varjon`},
		{`corpus[035] Edmond Hall`, `Edmond Hall`, `Edmond Hall`},
		{`corpus[036] Eduardo Marques`, `Eduardo Marques`, `Eduardo Marques`},
		{`corpus[037] Ella Fitzgerald - vocals  Paul Smith - p`, `Ella Fitzgerald - vocals  Paul Smith - piano`, `Ella Fitzgerald`},
		{`corpus[038] Ella Fitzgerald with Nelson Riddle And H`, `Ella Fitzgerald with Nelson Riddle And His Orchestra`, `Ella Fitzgerald`},
		{`corpus[039] Erroll Garner`, `Erroll Garner`, `Erroll Garner`},
		{`corpus[040] Festen`, `Festen`, `Festen`},
		{`corpus[041] Francoise Legrand`, `Francoise Legrand`, `Francoise Legrand`},
		{`corpus[042] Frank Gerard`, `Frank Gerard`, `Frank Gerard`},
		{`corpus[043] Frank Trumbauer`, `Frank Trumbauer`, `Frank Trumbauer`},
		{`corpus[044] Fritz Reiner Chicago Symphony`, `Fritz Reiner Chicago Symphony`, `Fritz Reiner Chicago Symphony`},
		{`corpus[045] Gaspar Claus`, `Gaspar Claus`, `Gaspar Claus`},
		{`corpus[046] Glenn Miller`, `Glenn Miller`, `Glenn Miller`},
		{`corpus[047] Harry James`, `Harry James`, `Harry James`},
		{`corpus[048] Herbert von Karajan  Wiener Philharmonik`, `Herbert von Karajan  Wiener Philharmoniker`, `Herbert von Karajan`},
		{`corpus[049] Hit Parade des Enfants`, `Hit Parade des Enfants`, `Hit Parade des Enfants`},
		{`corpus[050] Isabelle Aubret`, `Isabelle Aubret`, `Isabelle Aubret`},
		{`corpus[051] Isabelle De Funes`, `Isabelle De Funes`, `Isabelle De Funes`},
		{`corpus[052] Jacob Collier`, `Jacob Collier`, `Jacob Collier`},
		{`corpus[053] Jascha Horenstein - London Symphony Orch`, `Jascha Horenstein - London Symphony Orchestra`, `Jascha Horenstein`},
		{`corpus[054] Jascha Horenstein - Royal Philharmonic O`, `Jascha Horenstein - Royal Philharmonic Orchestra`, `Jascha Horenstein`},
		{`corpus[055] Jean Constantin`, `Jean Constantin`, `Jean Constantin`},
		{`corpus[056] Jean-Pierre Lang`, `Jean-Pierre Lang`, `Jean-Pierre Lang`},
		{`corpus[057] Jean-Pierre Sabar`, `Jean-Pierre Sabar`, `Jean-Pierre Sabar`},
		{`corpus[058] John Coltrane And Johnny Hartman`, `John Coltrane And Johnny Hartman`, `John Coltrane And Johnny Hartman`},
		{`corpus[059] Jude Kofie`, `Jude Kofie`, `Jude Kofie`},
		{`corpus[060] Leonard Bernstein`, `Leonard Bernstein`, `Leonard Bernstein`},
		{`corpus[061] Les Masques`, `Les Masques`, `Les Masques`},
		{`corpus[062] Louis Armstrong`, `Louis Armstrong`, `Louis Armstrong`},
		{`corpus[063] Luciano Pavarotti, Berliner Philharmonik`, `Luciano Pavarotti, Berliner Philharmoniker, Herbert von Karajan`, `Luciano Pavarotti`},
		{`corpus[064] Luciano Pavarotti, Coro del Teatro Comun`, `Luciano Pavarotti, Coro del Teatro Comunale di Bologna, Orchestra del Teatro Comunale di Bologna, Anton Guadagno`, `Luciano Pavarotti`},
		{`corpus[065] Luciano Pavarotti, English Chamber Orche`, `Luciano Pavarotti, English Chamber Orchestra, Richard Bonynge`, `Luciano Pavarotti`},
		{`corpus[066] Luciano Pavarotti, John Alldis Choir, Wa`, `Luciano Pavarotti, John Alldis Choir, Wandsworth School Boys Choir, London Philharmonic Orchestra, Zubin Mehta`, `Luciano Pavarotti`},
		{`corpus[067] Luciano Pavarotti, London Symphony Orche`, `Luciano Pavarotti, London Symphony Orchestra, Richard Bonynge`, `Luciano Pavarotti`},
		{`corpus[068] Luciano Pavarotti, Mirella Freni, Berlin`, `Luciano Pavarotti, Mirella Freni, Berliner Philharmoniker, Herbert von Karajan`, `Luciano Pavarotti`},
		{`corpus[069] Luciano Pavarotti, National Philharmonic`, `Luciano Pavarotti, National Philharmonic Orchestra, Giancarlo Chiaramello`, `Luciano Pavarotti`},
		{`corpus[070] Luciano Pavarotti, National Philharmonic`, `Luciano Pavarotti, National Philharmonic Orchestra, Nicola Rescigno`, `Luciano Pavarotti`},
		{`corpus[071] Luciano Pavarotti, New Philharmonia Orch`, `Luciano Pavarotti, New Philharmonia Orchestra, Richard Bonynge`, `Luciano Pavarotti`},
		{`corpus[072] Luciano Pavarotti, Nicolai Ghiaurov, Nat`, `Luciano Pavarotti, Nicolai Ghiaurov, National Philharmonic Orchestra, Robin Stapleton`, `Luciano Pavarotti`},
		{`corpus[073] Luciano Pavarotti, Orchestra del Teatro `, `Luciano Pavarotti, Orchestra del Teatro Comunale di Bologna, Richard Bonynge`, `Luciano Pavarotti`},
		{`corpus[074] Luciano Pavarotti, Orchestra of the Roya`, `Luciano Pavarotti, Orchestra of the Royal Opera House, Covent Garden, Edward Downes`, `Luciano Pavarotti`},
		{`corpus[075] Luciano Pavarotti, Philharmonia Orchestr`, `Luciano Pavarotti, Philharmonia Orchestra, Piero Gamba`, `Luciano Pavarotti`},
		{`corpus[076] Luciano Pavarotti, Wiener Opernorchester`, `Luciano Pavarotti, Wiener Opernorchester, Nicola Rescigno`, `Luciano Pavarotti`},
		{`corpus[077] Luciano Pavarotti, Wiener Philharmoniker`, `Luciano Pavarotti, Wiener Philharmoniker, Sir Georg Solti`, `Luciano Pavarotti`},
		{`corpus[078] Luciano Pavarotti, Wiener Volksopernorch`, `Luciano Pavarotti, Wiener Volksopernorchester, Leone Magiera`, `Luciano Pavarotti`},
		{`corpus[079] Magalie Noël`, `Magalie Noël`, `Magalie Noël`},
		{`corpus[080] Maria Callas`, `Maria Callas`, `Maria Callas`},
		{`corpus[081] Mark Knopfler`, `Mark Knopfler`, `Mark Knopfler`},
		{`corpus[082] Marpessa Dawn`, `Marpessa Dawn`, `Marpessa Dawn`},
		{`corpus[083] Miles Davis & company`, `Miles Davis & company`, `Miles Davis & company`},
		{`corpus[084] Miles Davis - Arranged and Directed by G`, `Miles Davis - Arranged and Directed by Gil Evans`, `Miles Davis`},
		{`corpus[085] Moby`, `Moby`, `Moby`},
		{`corpus[086] Moby, Apollo Jane, Deitrick Haddon, Mind`, `Moby, Apollo Jane, Deitrick Haddon, Mindy Jones`, `Moby`},
		{`corpus[087] Moby, Gregory Porter, Amythyst Kiah`, `Moby, Gregory Porter, Amythyst Kiah`, `Moby`},
		{`corpus[088] Moby, Jim James`, `Moby, Jim James`, `Moby`},
		{`corpus[089] Moby, Mark Lanegan, Kris Kristofferson`, `Moby, Mark Lanegan, Kris Kristofferson`, `Moby`},
		{`corpus[090] Moby, Mindy Jones`, `Moby, Mindy Jones`, `Moby`},
		{`corpus[091] Moby, Nataly Dawn, Alice Skye, Luna Li`, `Moby, Nataly Dawn, Alice Skye, Luna Li`, `Moby`},
		{`corpus[092] Moby, Novo Amor, Mindy Jones, Darlingsid`, `Moby, Novo Amor, Mindy Jones, Darlingside`, `Moby`},
		{`corpus[093] Moby, Skylar Grey, Darlingside`, `Moby, Skylar Grey, Darlingside`, `Moby`},
		{`corpus[094] Moby, Víkingur Ólafsson`, `Moby, Víkingur Ólafsson`, `Moby`},
		{`corpus[095] Nat King Cole`, `Nat King Cole`, `Nat King Cole`},
		{`corpus[096] Nat King Cole - with orchestra conducted`, `Nat King Cole - with orchestra conducted by Billy May`, `Nat King Cole`},
		{`corpus[097] New York Philharmonic`, `New York Philharmonic`, `New York Philharmonic`},
		{`corpus[098] Norah Jones`, `Norah Jones`, `Norah Jones`},
		{`corpus[099] Olivia Dean`, `Olivia Dean`, `Olivia Dean`},
		{`corpus[100] Pittsburgh Symphony Orchestra; Manfred H`, `Pittsburgh Symphony Orchestra; Manfred Honeck`, `Pittsburgh Symphony Orchestra; Manfred Honeck`},
		{`corpus[101] Pittsburgh Symphony Orchestra; Manfred H`, `Pittsburgh Symphony Orchestra; Manfred Honeck; Christina Landshamer; Jennifer Johnson Cano; Werner Güra; Shenyang; Mendelssohn Choir of Pittsburgh`, `Pittsburgh Symphony Orchestra; Manfred Honeck; Christina Landshamer; Jennifer Johnson Cano; Werner Güra; Shenyang; Mendelssohn Choir of Pittsburgh`},
		{`corpus[102] Queens Of The Stone Age`, `Queens Of The Stone Age`, `Queens Of The Stone Age`},
		{`corpus[103] Quintet of the Hot Club de France`, `Quintet of the Hot Club de France`, `Quintet of the Hot Club de France`},
		{`corpus[104] Red`, `Red`, `Red`},
		{`corpus[105] Royal Concertgebouw Orchestra`, `Royal Concertgebouw Orchestra`, `Royal Concertgebouw Orchestra`},
		{`corpus[106] Seiji Ozawa, Saito Kinen Orchestra`, `Seiji Ozawa, Saito Kinen Orchestra`, `Seiji Ozawa`},
		{`corpus[107] Sidney Bechet`, `Sidney Bechet`, `Sidney Bechet`},
		{`corpus[108] Simon Denizart`, `Simon Denizart`, `Simon Denizart`},
		{`corpus[109] Skeewiff`, `Skeewiff`, `Skeewiff`},
		{`corpus[110] Snarky Puppy`, `Snarky Puppy`, `Snarky Puppy`},
		{`corpus[111] Sophia Loren`, `Sophia Loren`, `Sophia Loren`},
		{`corpus[112] Stan Getz and João Gilberto featuring An`, `Stan Getz and João Gilberto featuring Antonio Carlos Jobim`, `Stan Getz and João Gilberto featuring Antonio Carlos Jobim`},
		{`corpus[113] Stevie Ray Vaughan & Double Trouble`, `Stevie Ray Vaughan & Double Trouble`, `Stevie Ray Vaughan & Double Trouble`},
		{`corpus[114] Sylvia Fels`, `Sylvia Fels`, `Sylvia Fels`},
		{`corpus[115] Test Signals`, `Test Signals`, `Test Signals`},
		{`corpus[116] The Con Tempo`, `The Con Tempo`, `The Con Tempo`},
		{`corpus[117] The Dave Brubeck Quartet`, `The Dave Brubeck Quartet`, `The Dave Brubeck Quartet`},
		{`corpus[118] The Harlem Footwarmers`, `The Harlem Footwarmers`, `The Harlem Footwarmers`},
		{`corpus[119] The Tommy Dorsey Orchestra`, `The Tommy Dorsey Orchestra`, `The Tommy Dorsey Orchestra`},
		{`corpus[120] Them Crooked Vultures`, `Them Crooked Vultures`, `Them Crooked Vultures`},
		{`corpus[121] Tommy Dorsey`, `Tommy Dorsey`, `Tommy Dorsey`},
		{`corpus[122] Various Interprets`, `Various Interprets`, `Various Interprets`},
		{`corpus[123] Wilbur de Paris`, `Wilbur de Paris`, `Wilbur de Paris`},
		{`corpus[124] toe`, `toe`, `toe`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Collapse(tc.raw); got != tc.want {
				t.Errorf("Collapse(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestCollapse_AdderleyKnownAcceptedImperfection asserts, explicitly and by
// itself (not just as a table row), that
// `Adderley - Coltrane - Chambers - Cobb - Kelly` collapses to `Adderley`.
//
// This is a KNOWN, ACCEPTED imperfection: the raw tag is a terse surname
// list from a Cannonball Adderley session, not "Cannonball Adderley" or any
// fuller name. The corpus's own measured ground truth (row 2 of the
// collapse-targets table in 02-ARTIST-CORPUS.md) expects exactly this naive
// first-segment result. Per D-06, Claude's discretion on whether a small
// hand-maintained exception list is warranted was exercised toward "uniform
// rule, no exception list" -- this plan does NOT special-case Adderley (or
// any other name) with a hand-maintained exception map. The uniform
// earliest-delimiter rule is what makes every one of the four join
// conventions collapse correctly with one code path, at the cost of this
// single acknowledged imperfection.
func TestCollapse_AdderleyKnownAcceptedImperfection(t *testing.T) {
	t.Parallel()

	const raw = "Adderley - Coltrane - Chambers - Cobb - Kelly"
	const want = "Adderley"

	if got := Collapse(raw); got != want {
		t.Errorf("Collapse(%q) = %q, want %q (known accepted imperfection, no exception list per D-06)", raw, got, want)
	}
}

// TestCollapse_FuzzSafety asserts Collapse never panics on adversarial or
// degenerate whitespace/delimiter-only inputs, satisfying threat T-02-01-02
// (Tampering: malformed/edge-case input must never panic).
func TestCollapse_FuzzSafety(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
	}{
		{"only spaces", "     "},
		{"only commas", ",,,,,"},
		{"only hyphens with spaces", " - - - "},
		{"only double-space runs", "        "},
		{"starts with comma delimiter", ", John Coltrane"},
		{"starts with spaced hyphen delimiter", " - Coltrane"},
		{"starts with with-delimiter", " with Orchestra"},
		{"starts with double-space delimiter", "  Orchestra"},
		{"single comma only", ","},
		{"single space", " "},
		{"tab and newline only", "\t\n"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Collapse(%q) panicked: %v", tc.raw, r)
				}
			}()
			_ = Collapse(tc.raw)
		})
	}
}
