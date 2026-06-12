package amt

// Compile-time assertion: *Service satisfies the Operator port.
//
// Pins the contract that the amt module's inbound port (Operator) is
// implemented by its concrete service. The api package consumes amt operations through
// amt.Operator rather than redeclaring the interface internally.
var _ Operator = (*Service)(nil)
