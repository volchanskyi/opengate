package amt

// Compile-time assertion: *Service satisfies the Operator port.
//
// Pins the contract that the amt module's inbound port (Operator) is
// implemented by its concrete service. Per ADR-020 §4.1 and the ADR-021
// §9 trigger row, the api package consumes amt operations through
// amt.Operator rather than redeclaring the interface internally.
var _ Operator = (*Service)(nil)
