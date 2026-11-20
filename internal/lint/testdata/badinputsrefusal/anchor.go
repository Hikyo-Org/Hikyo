package badinputsrefusal // want "inputs refused: (input digest mismatch|the driver did not pass -root and -inputs|undeclared input)"

// The anchor the input refusals run on: a stale digest, absent input flags and
// a path outside the declaration each make the analyzer refuse, never pass.
