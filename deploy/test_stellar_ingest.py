#!/usr/bin/env python3
"""Tests for stellar-ingest's MusicBrainz matcher.

    python3 -m unittest discover -s deploy -p 'test_*.py' -v

Offline by design: every MusicBrainz payload here is a trimmed capture of a real
response (only the fields the matcher reads), so the suite runs on the Mac and
on the Pi without touching the rate-limited API.

The regression these exist for: ingesting a folder called "daoud - ok" filed the
album as "Ok ok ok ok ok ok ok" by The Bombhappies (2007) instead of "ok" by
daoud (2025). The mechanism was not a close call between two pressings -- it was
the artist-less fallback query `release:"ok"`, which returns ten unrelated albums
all scoring >= 90, the top one at 100.
"""

from __future__ import annotations

import importlib.util
import sys
import types
import unittest
from pathlib import Path

# ---------------------------------------------------------------------------
# Import the script under test. Its filename is hyphenated (it is a command,
# not a module), and it hard-exits without mutagen, which the Mac lacks.
# The matcher never touches mutagen, so stub it only when it is genuinely absent.
# ---------------------------------------------------------------------------

try:  # pragma: no cover - environment dependent
    import mutagen  # noqa: F401
except ImportError:  # pragma: no cover
    for name, attrs in {
        "mutagen": ("version_string",),
        "mutagen.flac": ("FLAC", "Picture"),
        "mutagen.id3": ("ID3", "TALB", "TPE1", "TPE2", "TIT2", "TDRC", "TRCK", "TXXX"),
    }.items():
        stub = types.ModuleType(name)
        for attr in attrs:
            setattr(stub, attr, object)
        sys.modules[name] = stub

_SRC = Path(__file__).resolve().parent / "stellar-ingest.py"
_spec = importlib.util.spec_from_file_location("stellar_ingest", _SRC)
ingest = importlib.util.module_from_spec(_spec)
sys.modules["stellar_ingest"] = ingest  # @dataclass resolves via sys.modules
_spec.loader.exec_module(ingest)


def release(title, artist, score, status="Official", date=None, mbid=None,
            group=None):
    """A search result carrying only the fields the matcher reads.

    `group` is the release-group id. Left None it is omitted entirely, which
    exercises the title+credit fallback in _group_key.
    """
    r = {
        "id": mbid or f"id-{title}-{artist}".replace(" ", "-").lower(),
        "title": title,
        "score": score,
        "status": status,
        "date": date,
        "artist-credit": [{"name": artist, "joinphrase": ""}],
    }
    if group:
        r["release-group"] = {"id": group}
    return r


# Verbatim from `release:"ok"` (2026-08-19). This is the set the old code sorted
# by earliest-date, which is how the 2007 album won.
UNCONSTRAINED_OK = [
    release("Ok ok ok ok ok ok ok", "The Bombhappies", 100, date="2007-10-26",
            mbid="1e7c82b5-316b-44a2-8c39-98ccd6a90e46"),
    release("OK OK OK OK", "Colony House", 97, date="2025-08-01"),
    release("Ok, Ok, Ok", "Sista Mannen På Jorden", 95, date="2001-09-03"),
    release("OK OK OK", "Gilberto Gil", 95, status=None, date="2018-07-20"),
    release("OK OK OK", "Gilberto Gil", 95),
    release("OK OK OK", "Fitz and the Tantrums", 95),
    release("OK!OK!!", "栗林みえ", 92, date="1999-03-05"),
    release("OK OK", "HOKO", 92),
    release("OK OK", "HOKO", 92, date="2020-02-26"),
    release("Ok Ok", "TM Bax", 92),
]

# Verbatim from `release:"ok" AND artist:"daoud"` — the correct answer, which the
# folder name does supply an artist for.
CONSTRAINED_DAOUD = [
    release("ok", "daoud", 100, date="2025-08-29",
            mbid="de7b8a62-f383-4db6-af3c-49cb82e82dea"),
]


class TestNormaliseName(unittest.TestCase):
    def test_table(self):
        cases = [
            ("ok", "ok"),
            ("OK OK OK", "ok ok ok"),
            ("L'Oeil de Jules", "l oeil de jules"),
            ("la fièvre", "la fievre"),
            ("Ok, Ok, Ok", "ok ok ok"),
            ("  spaced   out  ", "spaced out"),
            ("", ""),
            # & folds to "and": the folder says one, MusicBrainz says the other.
            ("Cannonball & Coltrane", "cannonball and coltrane"),
            ("Cannonball and Coltrane", "cannonball and coltrane"),
            ("Louis Armstrong & Ella Fitzgerald",
             "louis armstrong and ella fitzgerald"),
        ]
        for raw, want in cases:
            with self.subTest(raw=raw):
                self.assertEqual(ingest.normalise_name(raw), want)


class TestArtistMatches(unittest.TestCase):
    def test_table(self):
        cases = [
            # (credit, queried, expected)
            ("daoud", "daoud", True),
            ("daoud", "Daoud", True),
            # The Karajan folder tags a conductor+orchestra pair; MusicBrainz
            # credits the conductor alone. Leading-token-run, so it matches.
            ("Herbert von Karajan", "Herbert von Karajan  Wiener Philharmoniker", True),
            ("Miles Davis Quintet", "Miles Davis", True),
            # Not a substring test: a guest credit must not pass as the artist.
            ("Louis Armstrong & Ella Fitzgerald", "Ella Fitzgerald", False),
            ("The Bombhappies", "daoud", False),
            ("daoud", "", False),
            ("", "daoud", False),
        ]
        for credit, queried, want in cases:
            with self.subTest(credit=credit, queried=queried):
                self.assertIs(
                    ingest.artist_matches(release("t", credit, 100), queried), want)

    def test_reads_multi_part_credits(self):
        r = {"artist-credit": [
            {"name": "Duke Ellington", "joinphrase": " & "},
            {"name": "John Coltrane", "joinphrase": ""},
        ]}
        self.assertEqual(ingest.release_artist_credit(r),
                         "Duke Ellington & John Coltrane")
        self.assertTrue(ingest.artist_matches(r, "Duke Ellington"))


class TestTitlesEquivalent(unittest.TestCase):
    def test_table(self):
        cases = [
            ("ok", "ok", True),
            ("OK", "ok", True),
            ("la fièvre", "la fievre", True),
            # The whole point: containment must not count.
            ("Ok ok ok ok ok ok ok", "ok", False),
            ("OK Computer", "ok", False),
            ("ok", "", False),
        ]
        for title, queried, want in cases:
            with self.subTest(title=title, queried=queried):
                self.assertIs(ingest.titles_equivalent(title, queried), want)


class TestChooseRelease(unittest.TestCase):
    def test_refuses_the_unconstrained_generic_title(self):
        """The exact regression: release:"ok" with no artist must yield nothing."""
        match, reason = ingest.choose_release(UNCONSTRAINED_OK, "", "ok")
        self.assertIsNone(match)
        self.assertIn("corroborated", reason)
        # And name what it rejected, so the report is diagnosable.
        self.assertIn("Bombhappies", reason)

    def test_accepts_the_artist_constrained_query(self):
        match, reason = ingest.choose_release(CONSTRAINED_DAOUD, "daoud", "ok")
        self.assertIsNotNone(match)
        self.assertEqual(match["id"], "de7b8a62-f383-4db6-af3c-49cb82e82dea")
        self.assertEqual(reason, "")

    def test_the_bombhappies_release_is_rejected_even_when_alone(self):
        """Not merely out-ranked -- affirmatively not a match for "ok" by daoud."""
        only_wrong = [UNCONSTRAINED_OK[0]]
        match, _ = ingest.choose_release(only_wrong, "daoud", "ok")
        self.assertIsNone(match)

    def test_below_threshold_is_refused(self):
        match, reason = ingest.choose_release(
            [release("ok", "daoud", 40)], "daoud", "ok")
        self.assertIsNone(match)
        self.assertIn("scored", reason)

    def test_empty_result_set(self):
        match, reason = ingest.choose_release([], "daoud", "ok")
        self.assertIsNone(match)

    def test_reissue_tie_breaks_to_earliest_official(self):
        """The tie-break still does its original job within one album."""
        reissues = [
            release("Kind of Blue", "Miles Davis", 100, date="1997-01-01"),
            release("Kind of Blue", "Miles Davis", 100, status="Bootleg", date="1959-01-01"),
            release("Kind of Blue", "Miles Davis", 100, date="1959-08-17",
                    mbid="the-first-press"),
            release("Kind of Blue", "Miles Davis", 100, date="2013-05-05"),
        ]
        match, reason = ingest.choose_release(reissues, "Miles Davis", "Kind of Blue")
        self.assertEqual(reason, "")
        self.assertEqual(match["id"], "the-first-press")

    def test_refuses_when_two_different_albums_tie(self):
        """Two corroborated but distinct releases at the same score is ambiguity."""
        tied = [
            release("Greatest Hits", "Queen", 100, date="1981-10-26"),
            release("Greatest Hits", "ABBA", 100, date="1975-11-17"),
        ]
        match, reason = ingest.choose_release(tied, "", "Greatest Hits")
        self.assertIsNone(match)
        self.assertIn("ambiguous", reason)

    def test_canonical_title_may_differ_when_the_artist_corroborates(self):
        """How an untidy folder name acquires a real title -- must keep working."""
        folder_title = "RStrauss Also Sprach Zarathustra Till Eulenspiegel Karajn VPO"
        results = [release("Also sprach Zarathustra / Till Eulenspiegel",
                           "Herbert von Karajan", 96, date="1974-01-01")]
        match, reason = ingest.choose_release(
            results, "Herbert von Karajan  Wiener Philharmoniker", folder_title)
        self.assertEqual(reason, "")
        self.assertEqual(match["title"], "Also sprach Zarathustra / Till Eulenspiegel")

    def test_ampersand_spelling_still_corroborates(self):
        """Real folder "Cannonball and Coltrane" vs MusicBrainz "Cannonball & Coltrane"."""
        results = [release("Cannonball & Coltrane",
                           "Cannonball Adderley & John Coltrane", 100,
                           date="1988-01-01", mbid="cannonball")]
        match, reason = ingest.choose_release(results, "", "Cannonball and Coltrane")
        self.assertEqual(reason, "")
        self.assertEqual(match["id"], "cannonball")

    def test_one_release_group_with_varying_credits_is_not_ambiguous(self):
        """Verbatim from `release:"Cannonball and Coltrane"` — three pressings.

        Same album, credited two different ways. Grouping on title+credit read
        this as a three-way ambiguity and refused; the release-group id says
        plainly that it is one album, so the reissue tie-break applies.
        """
        RG = "e5f68ad0-20af-3605-aa4d-089954ea2622"
        pressings = [
            release("Cannonball & Coltrane", "Cannonball Adderley & John Coltrane",
                    100, date="1964", group=RG, mbid="first-press"),
            release("Cannonball & Coltrane", "Cannonball Adderley & John Coltrane",
                    100, date="1991-08-20", group=RG),
            release("Cannonball & Coltrane", "Cannonball Adderley Quintet",
                    100, date="1988", group=RG),
        ]
        match, reason = ingest.choose_release(pressings, "", "Cannonball and Coltrane")
        self.assertEqual(reason, "")
        self.assertEqual(match["id"], "first-press")

    def test_distinct_release_groups_are_still_ambiguous(self):
        """Grouping on release-group must not swallow real ambiguity."""
        tied = [
            release("Greatest Hits", "Queen", 100, date="1981-10-26", group="rg-queen"),
            release("Greatest Hits", "ABBA", 100, date="1975-11-17", group="rg-abba"),
        ]
        match, reason = ingest.choose_release(tied, "", "Greatest Hits")
        self.assertIsNone(match)
        self.assertIn("ambiguous", reason)

    def test_lower_scoring_corroborated_beats_higher_scoring_stranger(self):
        """Score is a tiebreaker among real matches, not evidence of a match."""
        mixed = [
            release("Ok ok ok ok ok ok ok", "The Bombhappies", 100, date="2007-10-26"),
            release("ok", "daoud", 93, date="2025-08-29", mbid="right-one"),
        ]
        match, reason = ingest.choose_release(mixed, "daoud", "ok")
        self.assertEqual(reason, "")
        self.assertEqual(match["id"], "right-one")


class TestSearchRelease(unittest.TestCase):
    """search_release's candidate walk, with the HTTP layer replaced."""

    def setUp(self):
        self._real_http = ingest._http_json
        self.queries: list[str] = []

    def tearDown(self):
        ingest._http_json = self._real_http

    def stub(self, responder):
        def fake(url, attempts=1):
            self.queries.append(urllib_unquote(url))
            return responder(urllib_unquote(url))
        ingest._http_json = fake

    def test_walks_past_an_uncorroborated_hit_to_the_artist_query(self):
        """The daoud folder, end to end through the candidate walk."""
        def responder(url):
            if 'artist:"daoud"' in url:
                return {"releases": CONSTRAINED_DAOUD}
            if 'release:"ok"' in url:
                return {"releases": UNCONSTRAINED_OK}
            return {"releases": []}

        self.stub(responder)
        match, reason = ingest.search_release("", "ok", folder_name="daoud - ok")
        self.assertIsNotNone(match, reason)
        self.assertEqual(match["id"], "de7b8a62-f383-4db6-af3c-49cb82e82dea")

    def test_refuses_when_only_the_unconstrained_query_has_hits(self):
        """If the artist query genuinely misses, refuse -- do not take the stranger."""
        def responder(url):
            if 'artist:' in url:
                return {"releases": []}
            return {"releases": UNCONSTRAINED_OK}

        self.stub(responder)
        match, reason = ingest.search_release("", "ok", folder_name="daoud - ok")
        self.assertIsNone(match)
        self.assertIn("corroborated", reason)

    def test_network_failure_aborts_instead_of_degrading(self):
        """A blip on a strong query must not hand the answer to a weaker one."""
        def responder(url):
            if 'artist:"daoud"' in url:
                return None          # exhausted its attempts
            return {"releases": UNCONSTRAINED_OK}

        self.stub(responder)
        with self.assertRaises(ingest.SearchUnavailable):
            ingest.search_release("", "ok", folder_name="daoud - ok")

    def test_reason_lists_every_query_tried(self):
        self.stub(lambda url: {"releases": []})
        match, reason = ingest.search_release("", "ok", folder_name="daoud - ok")
        self.assertIsNone(match)
        self.assertEqual(reason.count("->"), len(self.queries))


class TestCandidateQueries(unittest.TestCase):
    """Guards the folder-name reader the matcher depends on."""

    def test_daoud_folder_yields_an_artist_bearing_candidate(self):
        cands = ingest.candidate_queries("daoud - ok", "")
        self.assertIn(("daoud", "ok"), cands)

    def test_strips_quality_decorations(self):
        """Every folder below is a real directory name on the library SSD.

        Fixtures invented from the pattern's own shape prove only that the
        pattern matches itself; two of the bugs here were found by running the
        real inventory through the cleaner and reading the output.
        """
        cases = [
            ("RStrauss Also Sprach Zarathustra Till Eulenspiegel Karajn VPO"
             "__FLAC_352k-24b",
             "RStrauss Also Sprach Zarathustra Till Eulenspiegel Karajn VPO"),
            # 11289k is a DSD256 rate. Six digits, so the sample-rate pattern
            # must consume all of them -- a four-digit cap matched only "1289k"
            # and left "-1" glued to the title.
            ("HDTT13879 Miles Davis - Kind Of Blue-DSF-11289k-1b",
             "Miles Davis - Kind Of Blue"),
            ("Cannonball and Coltrane-FLAC-352k-24b",
             "Cannonball and Coltrane"),
            # "DSF256" -- no word boundary between the name and the rate, so
            # `dsf\b` never fired.
            ("Ella Fitzgerald Swings Brightly with Nelson-DSF256",
             "Ella Fitzgerald Swings Brightly with Nelson"),
            # "DFS" is a typo for DSF in the real folder name. Tolerate it.
            ("John Coltrane And Johnny Hartman-DFS256",
             "John Coltrane And Johnny Hartman"),
            # Bare numbers inside a hyphenated run of decorations go with it.
            ("Rachmaninoff-Sumphonic-Dances-096-HD-176-WAV",
             "Rachmaninoff-Sumphonic-Dances"),
            # ... but a space-separated number is part of the title. "+ 19" is
            # the size of Gil Evans's orchestra, not a bit depth.
            ("Miles Ahead - Miles Davis + 19-DSF-11289k-1b",
             "Miles Ahead - Miles Davis + 19"),
            # A qualifier can follow the decorations, so they are not always
            # at the end of the name.
            ("Miles Davis - Kind Of Blue-FLAC-352k-24b Corrected Speed",
             "Miles Davis - Kind Of Blue Corrected Speed"),
            ("Strauss Also Sprach Zarathustra - Fritz Reiner Chicago Symphony"
             " (1962 Recording)-DSF-11289k-1b",
             "Strauss Also Sprach Zarathustra - Fritz Reiner Chicago Symphony"
             " (1962 Recording)"),
            ("toe - The Future Is Now - FLAC", "toe - The Future Is Now"),
            ("toe - The Future Is Now - WAV", "toe - The Future Is Now"),
            # "NativeDSD" is a record label. Stripping decorations out of the
            # middle of a word truncates it to "Native".
            ("NativeDSD", "NativeDSD"),
            # Nothing to strip: leave clean names exactly alone.
            ("daoud - ok", "daoud - ok"),
            ("Duke Ellington, John Coltrane", "Duke Ellington, John Coltrane"),
            ("Mahler Symphony No 8", "Mahler Symphony No 8"),
            ("Djesse Vol. 4", "Djesse Vol. 4"),
            ("Sigxer SU-6 test", "Sigxer SU-6 test"),
        ]
        for folder, want in cases:
            with self.subTest(folder=folder):
                self.assertEqual(ingest.candidate_album_title(folder), want)

    def test_splits_on_en_dash_as_well_as_hyphen(self):
        """A real folder uses an en dash, which yielded no artist at all."""
        for folder in ("Nat King Cole – Just One Of Those Things"
                       "-DSF-11289k-1b",
                       "Nat King Cole - Just One Of Those Things"):
            with self.subTest(folder=folder):
                cands = ingest.candidate_queries(folder, "")
                self.assertIn(("Nat King Cole", "Just One Of Those Things"),
                              cands)


def urllib_unquote(url: str) -> str:
    import urllib.parse
    return urllib.parse.unquote(url)


if __name__ == "__main__":
    unittest.main(verbosity=2)
