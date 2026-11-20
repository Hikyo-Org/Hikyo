package main

import "encoding/json/v2"

var stableJSON = json.JoinOptions(json.Deterministic(true))

func main() {}
