package ingest

// SchemaVersion is the stellar-ingest --json document version this package
// understands. The script writes it into every document; a mismatch means the
// deployed script and the deployed binary drifted apart.
const SchemaVersion = 1

// Item is one inbox entry's outcome. Field names mirror the script's JSON
// exactly; the socket layer re-exports this shape unchanged.
type Item struct {
	Name          string   `json:"name"`
	Status        string   `json:"status"` // ingested | would-ingest | refused | skipped
	Reason        string   `json:"reason"`
	Target        string   `json:"target"`
	AudioFiles    int      `json:"audioFiles"`
	Tagged        []string `json:"tagged"`
	TagFailures   []string `json:"tagFailures"`
	MD5Mismatches []string `json:"md5Mismatches"`
	MBRelease     string   `json:"mbRelease"`
	Art           string   `json:"art"`
	Notes         []string `json:"notes"`
}

// Summary is the per-run tally.
type Summary struct {
	Total        int `json:"total"`
	Ingested     int `json:"ingested"`
	WouldIngest  int `json:"wouldIngest"`
	Refused      int `json:"refused"`
	Skipped      int `json:"skipped"`
	TagFailures  int `json:"tagFailures"`
	AudioAltered int `json:"audioAltered"`
}

// Report is the whole stellar-ingest --json document, plus the plan token the
// backend attaches on a preview run.
type Report struct {
	Schema   int            `json:"schema"`
	DryRun   bool           `json:"dryRun"`
	Error    string         `json:"error"`
	ExitCode int            `json:"exitCode"`
	Items    []Item         `json:"items"`
	Summary  Summary        `json:"summary"`
	MPD      map[string]int `json:"mpd"`

	// Token is set by Preview and must be handed back to Commit. It is not
	// produced by the script.
	Token string `json:"token,omitempty"`
}

// Status is the cheap answer to "is there anything to ingest?" — it lists the
// inbox without running the script, so the UI can poll it freely.
type Status struct {
	Items     []string `json:"items"`
	Count     int      `json:"count"`
	Busy      bool     `json:"busy"`
	Available bool     `json:"available"` // script and inbox both present
	Error     string   `json:"error,omitempty"`
}
