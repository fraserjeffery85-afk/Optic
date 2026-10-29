package main

// The version lives here and nowhere else. The release workflow reads it, so
// raising it (and adding a section to CHANGELOG.md) is how a release is made.
const appVersion = "1.1.0"

const (
	appName       = "Optic"
	appCompany    = "OB Industries"
	legacyAppName = "ClearRide" // the app's earlier name; settings are migrated from it
)
