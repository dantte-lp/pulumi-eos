package l3

// EOS list-type tokens shared by `eos:l3:CommunityList` and
// `eos:l3:ExtCommunityList`. Kept as constants so the strings appear in
// exactly one place per the goconst lint policy and so a future EOS
// keyword change touches one file.
const (
	listTypeStandard = "standard"
	listTypeRegexp   = "regexp"
)
