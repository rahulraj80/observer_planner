package natslog

// TestStoreDir exposes testStoreDir to the external conformance test
// (package natslog_test), so its broker store survives the same late-write
// race (T-312).
var TestStoreDir = testStoreDir
