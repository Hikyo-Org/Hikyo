package audit

const (
	EventPrivacySubjectCorrected  EventType = "privacy.subject_corrected"
	EventPrivacySubjectReleased   EventType = "privacy.subject_released"
	EventPrivacySubjectExported   EventType = "privacy.subject_exported"
	EventPrivacySubjectRestricted EventType = "privacy.subject_restricted"
	EventPrivacySubjectErased     EventType = "privacy.subject_erased"
)

var privacySubjectSpec = TypeSpec{
	SchemaVersion: 1, Retention: RetentionSecurity,
	Outcomes: map[Outcome]bool{OutcomeSuccess: true}, Trails: map[Trail]bool{TrailInstance: true},
	Schema: Schema{"target_principal": {Kind: KindString, Required: true}, "authority": {Kind: KindString, Required: true, Enum: []string{"local-host"}}},
}
