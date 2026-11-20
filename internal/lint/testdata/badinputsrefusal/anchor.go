package badinputsrefusal // want "inputs refused: (input digest mismatch|the driver did not pass -root and -inputs|undeclared input|-root does not contain the anchor package)"

// The anchor the input refusals run on: a stale digest, absent input flags, a
// path outside the declaration and a -root that does not contain this package
// each make the analyzer refuse, never pass.
