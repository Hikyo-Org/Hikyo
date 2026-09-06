package domain

// SingletonTopology is an enrolled process identity, not a replica count.
type SingletonTopology struct {
	HA     bool   `json:"ha"`
	NodeID string `json:"node_id"`
}

// SingletonTopologyChange retains preparation and installation identities.
// The existing configuration generation fences each committed assignment.
type SingletonTopologyChange struct {
	Before SingletonTopology `json:"before"`
	After  SingletonTopology `json:"after"`
}
