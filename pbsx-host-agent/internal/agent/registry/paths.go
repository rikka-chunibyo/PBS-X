package registry

const CONFIG = "Software\\PBS-X\\Config"
const AUTH = "Software\\PBS-X\\Auth"

// Legacy paths are read during migration only. PBS-X never writes new state there.
const legacyConfig = "Software\\PBSPlus\\Config"
const legacyAuth = "Software\\PBSPlus\\Auth"
