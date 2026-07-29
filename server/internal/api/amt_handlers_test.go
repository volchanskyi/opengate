package api

const (
	testAMTEmail   = "admin@test.com"
	testPathAMTOne = "/api/v1/amt/devices/"
)

// Power actions are the AMT module's whole HTTP surface. Discovery is served by
// the device read — a device carries its AMT property in its own payload — and
// is covered in device_handlers_test.go. The power-action cases live in
// amt_handlers_part2_test.go.
