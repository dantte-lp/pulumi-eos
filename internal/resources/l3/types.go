package l3

// EOS list-type tokens shared by `eos:l3:CommunityList` and
// `eos:l3:ExtCommunityList`. Kept as constants so the strings appear in
// exactly one place per the goconst lint policy and so a future EOS
// keyword change touches one file.
const (
	listTypeStandard = "standard"
	listTypeRegexp   = "regexp"
)

// EOS interface admin-state keywords. `shutdown` recurs across every
// resource that owns an interface (l2 Interface, l2 PortChannel, l3
// Subinterface, l3 RouterOspf, l3 GreTunnel, …). Centralised so the
// CLI keyword exists in one place.
const (
	keywordShutdown   = "shutdown"
	keywordNoShutdown = "no shutdown"
)
